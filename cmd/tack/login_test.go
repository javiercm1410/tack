package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javiercm1410/tack/internal/config"
	"github.com/javiercm1410/tack/internal/slack"
)

const (
	testToken  = "xoxc-1111-2222-login-token"
	testCookie = "xoxd-" + "abcdefghij%2Fklmnopqrstuvwxyz0123456789abcdefghij%2Bklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789%3D%3D"
)

func secrets(values ...string) secretReader {
	return func() (string, error) {
		if len(values) == 0 {
			return "", os.ErrClosed
		}
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

func okVerify(seen *slack.Credentials) verifier {
	return func(_ context.Context, c slack.Credentials) (slack.Identity, error) {
		*seen = c
		return slack.Identity{User: "jdoe", Team: "Example"}, nil
	}
}

func loginFixture(t *testing.T, vars map[string]string) (string, func(string) string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tack", "config.toml")
	vars["TACK_CONFIG"] = path
	return path, func(k string) string { return vars[k] }
}

func TestLoginExistingProfileSavesExports(t *testing.T) {
	path, getenv := loginFixture(t, map[string]string{"TOKEN": "xoxc-old", "COOKIE": "xoxd-old"})
	cfg := config.Config{DefaultProfile: "work", Profiles: map[string]config.Profile{
		"work":     {WorkspaceURL: "https://work.slack.com", TokenEnv: "TOKEN", CookieEnv: "COOKIE"},
		"personal": {WorkspaceURL: "https://personal.slack.com", TokenEnv: "P_TOKEN", CookieEnv: "P_COOKIE"},
	}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	file := credentialsPath(path)
	if err := os.WriteFile(file, []byte("export P_TOKEN='xoxc-keep'\nexport TOKEN='xoxc-stale'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var seen slack.Credentials
	var out, errOut bytes.Buffer
	// Empty answer selects the default profile.
	code := runLogin(context.Background(), options{command: "login"}, true, loginIO{input: strings.NewReader("\n"), readSecret: secrets(" "+testToken+" \n", testCookie), verify: okVerify(&seen)}, &out, &errOut, getenv)
	if code != 0 {
		t.Fatal(code, errOut.String())
	}
	if seen.Token != testToken || seen.Cookie != testCookie || seen.BaseURL != "https://work.slack.com" {
		t.Fatalf("verified wrong credentials: %+v", seen)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"export P_TOKEN='xoxc-keep'", "export TOKEN='" + testToken + "'", "export COOKIE='" + testCookie + "'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "xoxc-stale") {
		t.Fatal("old value kept")
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0600 {
		t.Fatal("credentials file not private:", info.Mode())
	}
	text := out.String()
	if strings.Contains(text, testToken) || strings.Contains(text, testCookie) {
		t.Fatal("printed a credential value")
	}
	for _, want := range []string{"Logged in to Example as jdoe (profile work)", "source ", "still exports the old values", "localConfig_v2", "work.slack.com"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in output:\n%s", want, text)
		}
	}
}

func TestLoginCreatesFirstProfile(t *testing.T) {
	path, getenv := loginFixture(t, map[string]string{})
	var seen slack.Credentials
	var out, errOut bytes.Buffer
	answers := "https://acme.slack.com\n\n\n\ny\n"
	code := runLogin(context.Background(), options{command: "login", args: []string{"acme"}}, true, loginIO{input: strings.NewReader(answers), readSecret: secrets(testToken, testCookie), verify: okVerify(&seen)}, &out, &errOut, getenv)
	if code != 0 {
		t.Fatal(code, errOut.String(), out.String())
	}
	cfg, _, err := config.Load(getenv, false)
	if err != nil || cfg.DefaultProfile != "acme" || cfg.Profiles["acme"].TokenEnv != "TACK_ACME_XOXC_TOKEN" {
		t.Fatal(cfg, err)
	}
	data, _ := os.ReadFile(credentialsPath(path))
	if !strings.Contains(string(data), "export TACK_ACME_XOXC_TOKEN='"+testToken+"'") {
		t.Fatal(string(data))
	}
}

func TestLoginRejectedSessionSavesNothing(t *testing.T) {
	path, getenv := loginFixture(t, map[string]string{})
	cfg := config.Config{DefaultProfile: "work", Profiles: map[string]config.Profile{"work": {WorkspaceURL: "https://work.slack.com", TokenEnv: "TOKEN", CookieEnv: "COOKIE"}}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	reject := func(context.Context, slack.Credentials) (slack.Identity, error) {
		return slack.Identity{}, slack.APIError("auth.test", "invalid_auth")
	}
	var out, errOut bytes.Buffer
	code := runLogin(context.Background(), options{command: "login", args: []string{"work"}}, true, loginIO{input: strings.NewReader(""), readSecret: secrets(testToken, testCookie), verify: reject}, &out, &errOut, getenv)
	if code != 2 || !strings.Contains(errOut.String(), "nothing was saved") {
		t.Fatal(code, errOut.String())
	}
	if _, err := os.Stat(credentialsPath(path)); !os.IsNotExist(err) {
		t.Fatal("credentials file written after a rejected session")
	}
}

func TestLoginRejectsBadInput(t *testing.T) {
	path, getenv := loginFixture(t, map[string]string{})
	cfg := config.Config{Profiles: map[string]config.Profile{"work": {WorkspaceURL: "https://work.slack.com", TokenEnv: "TOKEN", CookieEnv: "COOKIE"}}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	never := func(context.Context, slack.Credentials) (slack.Identity, error) {
		t.Fatal("verify called")
		return slack.Identity{}, nil
	}
	cases := []struct {
		name        string
		o           options
		interactive bool
		read        secretReader
		code        int
	}{
		{"json", options{command: "login", json: true}, true, secrets(), 1},
		{"piped", options{command: "login"}, false, secrets(), 1},
		{"swapped values", options{command: "login", args: []string{"work"}}, true, secrets(testCookie, testToken), 2},
		{"quote in value", options{command: "login", args: []string{"work"}}, true, secrets("xoxc-a'b", testCookie), 2},
		{"cancelled", options{command: "login", args: []string{"work"}}, true, secrets(), 1},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		code := runLogin(context.Background(), tc.o, tc.interactive, loginIO{input: strings.NewReader(""), readSecret: tc.read, verify: never}, &out, &errOut, getenv)
		if code != tc.code {
			t.Errorf("%s: code %d, want %d: %s%s", tc.name, code, tc.code, out.String(), errOut.String())
		}
	}
	if _, err := os.Stat(credentialsPath(path)); !os.IsNotExist(err) {
		t.Fatal("credentials file written for bad input")
	}
}

func TestShellSourcesCredentials(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "credentials.env")
	if err := saveCredentials(file, map[string]string{"A_TOKEN": testToken, "A_COOKIE": testCookie}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if strings.Count(string(data), "export ") != 2 {
		t.Fatal(string(data))
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	out, err := execShell(". " + shellQuote(file) + ` && printf '%s|%s' "$A_TOKEN" "$A_COOKIE"`)
	if err != nil || out != testToken+"|"+testCookie {
		t.Fatalf("sourced %q, %v", out, err)
	}
}

func execShell(script string) (string, error) {
	out, err := exec.Command("/bin/sh", "-c", script).Output()
	return string(out), err
}
