package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javiercm1410/tack/internal/slack"
)

func TestResolve(t *testing.T) {
	const document = `default_profile = "accela"
[profiles.accela]
workspace_url = "https://accela.slack.com"
token_env = "SLACK_XOXC_TOKEN"
cookie_env = "SLACK_XOXD_TOKEN"
[profiles.other]
workspace_url = "https://other.slack.com"
token_env = "OTHER_TOKEN"
cookie_env = "OTHER_COOKIE"
`
	for _, tc := range []struct {
		name, flag, env, doc, want string
		code                       int
	}{
		{name: "legacy", want: "legacy"},
		{name: "default", doc: document, want: "https://accela.slack.com"},
		{name: "flag", flag: "other", env: "accela", doc: document, want: "https://other.slack.com"},
		{name: "environment", env: "other", doc: document, want: "https://other.slack.com"},
		{name: "unknown", flag: "missing", doc: document, code: 1},
		{name: "missing config selected", flag: "other", code: 1},
		{name: "malformed", doc: "token = [secret", code: 1},
		{name: "invalid URL", doc: strings.ReplaceAll(document, "https://accela.slack.com", "https://evil.example"), code: 1},
		{name: "missing env reference", doc: strings.ReplaceAll(document, `token_env = "SLACK_XOXC_TOKEN"`, `token_env = ""`), code: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			vars := map[string]string{"HOME": home, "SLACK_WORKSPACE_URL": "legacy", "SLACK_XOXC_TOKEN": "legacy-token", "SLACK_XOXD_TOKEN": "legacy-cookie", "TACK_PROFILE": tc.env}
			if tc.doc != "" {
				path := filepath.Join(home, ".config", "tack")
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "config.toml"), []byte(tc.doc), 0600); err != nil {
					t.Fatal(err)
				}
			}
			get, err := Resolve(func(k string) string { return vars[k] }, tc.flag)
			if (tc.code == 0 && err != nil) || (tc.code != 0 && (err == nil || slack.ExitCode(err) != tc.code)) {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				if strings.Contains(err.Error(), "secret") {
					t.Fatal("leaked configuration")
				}
				return
			}
			if get("SLACK_WORKSPACE_URL") != tc.want {
				t.Fatal("wrong workspace")
			}
			if tc.want == "https://other.slack.com" && get("SLACK_XOXC_TOKEN") != "" {
				t.Fatal("borrowed legacy credentials")
			}
		})
	}
}
func TestExplicitPathAndXDG(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"TACK_CONFIG", "XDG_CONFIG_HOME"} {
		vars := map[string]string{key: root}
		if key == "TACK_CONFIG" {
			vars[key] = filepath.Join(root, "missing.toml")
		}
		_, err := Resolve(func(k string) string { return vars[k] }, "")
		if key == "TACK_CONFIG" && err == nil {
			t.Fatal("ignored explicit missing config")
		}
		if key == "XDG_CONFIG_HOME" && err != nil {
			t.Fatal(err)
		}
	}
}
