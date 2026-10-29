// Package config resolves workspace profiles without storing session credentials.
package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/javiercm1410/tack/internal/slack"
	"github.com/pelletier/go-toml/v2"
)

type Profile struct {
	WorkspaceURL string `toml:"workspace_url"`
	TokenEnv     string `toml:"token_env"`
	CookieEnv    string `toml:"cookie_env"`
}
type Config struct {
	DefaultProfile string             `toml:"default_profile"`
	Profiles       map[string]Profile `toml:"profiles"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Resolve selects flag > TACK_PROFILE > default_profile. Without selection it
// preserves the legacy environment. A selected profile never borrows credentials.
func Resolve(getenv func(string) string, selected string) (func(string) string, error) {
	path := getenv("TACK_CONFIG")
	explicitPath := path != ""
	if path == "" {
		base := getenv("XDG_CONFIG_HOME")
		if base == "" {
			home := getenv("HOME")
			if home == "" {
				home = getenv("USERPROFILE")
			}
			if home != "" {
				base = filepath.Join(home, ".config")
			}
		}
		if base != "" {
			path = filepath.Join(base, "tack", "config.toml")
		}
	}
	if selected == "" {
		selected = getenv("TACK_PROFILE")
	}
	var cfg Config
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) || explicitPath {
				return nil, slack.Usage("cannot read tack profile configuration")
			}
		} else {
			// Do not expose parser diagnostics: they can contain configuration values.
			if toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&cfg) != nil {
				return nil, slack.Usage("invalid TOML in tack profile configuration")
			}
		}
	}
	if selected == "" {
		selected = cfg.DefaultProfile
	}
	if selected == "" {
		return getenv, nil
	}
	if !namePattern.MatchString(selected) {
		return nil, slack.Usage("invalid profile name; use letters, digits, underscores, or hyphens")
	}
	profile, ok := cfg.Profiles[selected]
	if !ok {
		return nil, slack.Usage("selected profile does not exist in tack configuration")
	}
	if slack.ValidateURL(profile.WorkspaceURL) != nil {
		return nil, slack.Usage("profile workspace_url must be an HTTPS Slack workspace URL")
	}
	if !envPattern.MatchString(profile.TokenEnv) || !envPattern.MatchString(profile.CookieEnv) {
		return nil, slack.Usage("profile requires token_env and cookie_env environment variable names")
	}
	return func(key string) string {
		switch key {
		case "SLACK_WORKSPACE_URL":
			return profile.WorkspaceURL
		case "SLACK_XOXC_TOKEN":
			return getenv(profile.TokenEnv)
		case "SLACK_XOXD_TOKEN":
			return getenv(profile.CookieEnv)
		default:
			return getenv(key)
		}
	}, nil
}
