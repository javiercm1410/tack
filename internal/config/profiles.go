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
	cfg, _, err := Load(getenv, false)
	if err != nil {
		return nil, err
	}
	if selected == "" {
		selected = getenv("TACK_PROFILE")
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
	if err := ValidateProfile(profile); err != nil {
		return nil, err
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

// Load reads the profile file. Setup commands may create an explicitly named file.
func Load(getenv func(string) string, allowMissing bool) (Config, string, error) {
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
	var cfg Config
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) || (explicitPath && !allowMissing) {
				return cfg, path, slack.Usage("cannot read tack profile configuration")
			}
		} else {
			// Do not expose parser diagnostics: they can contain configuration values.
			if toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&cfg) != nil {
				return cfg, path, slack.Usage("invalid TOML in tack profile configuration")
			}
		}
	}
	return cfg, path, nil
}

// Validate checks all profiles without reading their credential variables.
func Validate(cfg Config) error {
	for name, p := range cfg.Profiles {
		if !namePattern.MatchString(name) {
			return slack.Usage("invalid profile name; use letters, digits, underscores, or hyphens")
		}
		if err := ValidateProfile(p); err != nil {
			return err
		}
	}
	if cfg.DefaultProfile != "" {
		if _, ok := cfg.Profiles[cfg.DefaultProfile]; !ok {
			return slack.Usage("default profile does not exist")
		}
	}
	return nil
}
func ValidName(name string) bool { return namePattern.MatchString(name) }
func ValidateProfile(p Profile) error {
	if slack.ValidateURL(p.WorkspaceURL) != nil {
		return slack.Usage("profile workspace_url must be an HTTPS Slack workspace URL")
	}
	if !envPattern.MatchString(p.TokenEnv) || !envPattern.MatchString(p.CookieEnv) {
		return slack.Usage("profile requires token_env and cookie_env environment variable names")
	}
	if p.TokenEnv == p.CookieEnv {
		return slack.Usage("token_env and cookie_env must use different variables")
	}
	return nil
}

// Save atomically replaces the configuration with a private TOML file.
func Save(path string, cfg Config) error {
	if path == "" {
		return slack.Usage("set HOME, USERPROFILE, XDG_CONFIG_HOME, or TACK_CONFIG to configure profiles")
	}
	if err := Validate(cfg); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return slack.Usage("cannot encode tack configuration")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return slack.Usage("cannot create tack configuration directory")
	}
	file, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return slack.Usage("cannot write tack configuration")
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return slack.Usage("cannot write tack configuration")
	}
	if err = file.Close(); err != nil {
		return slack.Usage("cannot close tack configuration")
	}
	if err = os.Rename(temp, path); err != nil {
		return slack.Usage("cannot replace tack configuration")
	}
	return nil
}
