package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javiercm1410/tack/internal/config"
)

func configFixture(t *testing.T) (string, func(string) string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	vars := map[string]string{"TACK_CONFIG": path, "TOKEN": "xoxc-private-token", "COOKIE": "xoxd-private-cookie"}
	return path, func(k string) string { return vars[k] }
}
func TestConfigOnboardAndDefault(t *testing.T) {
	path, getenv := configFixture(t)
	var out, stderr bytes.Buffer
	first := "accela\nhttps://accela.slack.com\nTOKEN\nCOOKIE\n\n\n"
	code := runConfig(options{command: "config", args: []string{"add"}}, strings.NewReader(first), true, &out, &stderr, getenv)
	if code != 0 {
		t.Fatal(code, stderr.String())
	}
	cfg, _, err := config.Load(getenv, false)
	if err != nil || cfg.DefaultProfile != "accela" || len(cfg.Profiles) != 1 {
		t.Fatal(cfg, err)
	}
	second := "https://personal.slack.com\n\n\n\n\n"
	out.Reset()
	stderr.Reset()
	code = runConfig(options{args: []string{"add", "personal"}}, strings.NewReader(second), true, &out, &stderr, getenv)
	if code != 0 {
		t.Fatal(code, stderr.String())
	}
	cfg, _, err = config.Load(getenv, false)
	if err != nil || cfg.DefaultProfile != "accela" || len(cfg.Profiles) != 2 || cfg.Profiles["personal"].TokenEnv != "TACK_PERSONAL_XOXC_TOKEN" {
		t.Fatal(cfg, err)
	}
	out.Reset()
	stderr.Reset()
	code = runConfig(options{args: []string{"default", "personal"}, json: true}, nil, false, &out, &stderr, getenv)
	if code != 0 || !json.Valid(out.Bytes()) {
		t.Fatal(code, out.String(), stderr.String())
	}
	cfg, _, _ = config.Load(getenv, false)
	if cfg.DefaultProfile != "personal" {
		t.Fatal("default not saved")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") {
		t.Fatal("saved credential values")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("configuration permissions not private")
	}
}
func TestConfigSafeJSONWithoutAuth(t *testing.T) {
	path, getenv := configFixture(t)
	cfg := config.Config{DefaultProfile: "work", Profiles: map[string]config.Profile{"work": {WorkspaceURL: "https://work.slack.com", TokenEnv: "TOKEN", CookieEnv: "COOKIE"}}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run(context.Background(), []string{"config", "--json"}, &out, &stderr, getenv); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "private") {
		t.Fatal(out.String())
	}
	var payload struct {
		ActiveProfile string `json:"active_profile"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil || payload.ActiveProfile != "work" {
		t.Fatal(out.String())
	}
}
func TestConfigCancellationAndErrorsPreserveFile(t *testing.T) {
	for _, tc := range []struct {
		name              string
		args              []string
		input             string
		interactive, json bool
	}{
		{"cancel", []string{"add", "new"}, "https://new.slack.com\n\n\n\nn\n", true, false},
		{"eof", []string{"add"}, "new\n", true, false},
		{"duplicate", []string{"add", "work"}, "", true, false},
		{"bad url", []string{"add", "new"}, "https://evil.example\n\n\n", true, false},
		{"noninteractive", []string{"add"}, "", false, false},
		{"json setup", []string{"add"}, "", true, true},
		{"missing default", []string{"default", "missing"}, "", false, true},
		{"unknown action", []string{"delete"}, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, getenv := configFixture(t)
			cfg := config.Config{DefaultProfile: "work", Profiles: map[string]config.Profile{"work": {WorkspaceURL: "https://work.slack.com", TokenEnv: "TOKEN", CookieEnv: "COOKIE"}}}
			if err := config.Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			var out, stderr bytes.Buffer
			code := runConfig(options{args: tc.args, json: tc.json}, strings.NewReader(tc.input), tc.interactive, &out, &stderr, getenv)
			if code != 1 {
				t.Fatal(code, out.String(), stderr.String())
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("failed setup changed configuration")
			}
			if tc.json && !json.Valid(out.Bytes()) {
				t.Fatal("invalid JSON error")
			}
		})
	}
}
