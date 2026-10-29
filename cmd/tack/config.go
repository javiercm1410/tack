package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/javiercm1410/tack/internal/config"
	"github.com/javiercm1410/tack/internal/slack"
)

func runConfig(o options, input io.Reader, interactive bool, out, errOut io.Writer, getenv func(string) string) int {
	action := "show"
	if len(o.args) > 0 {
		action = o.args[0]
	}
	if (action != "show" && action != "add" && action != "default") || (action == "show" && len(o.args) > 1) || (action == "default" && len(o.args) != 2) {
		return fail(out, errOut, o.json, slack.Usage("usage: tack config [show | add [NAME] | default NAME]"), nil)
	}
	if action == "add" && (o.json || !interactive) {
		return fail(out, errOut, o.json, slack.Usage("config add requires an interactive terminal; edit TOML for automated setup"), nil)
	}
	cfg, path, err := config.Load(getenv, true)
	if err != nil {
		return fail(out, errOut, o.json, err, nil)
	}
	if err = config.Validate(cfg); err != nil {
		return fail(out, errOut, o.json, err, nil)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]config.Profile{}
	}
	if action == "default" {
		name := o.args[1]
		if _, ok := cfg.Profiles[name]; !ok {
			return fail(out, errOut, o.json, slack.Usage("profile does not exist"), nil)
		}
		cfg.DefaultProfile = name
		if err = config.Save(path, cfg); err != nil {
			return fail(out, errOut, o.json, err, nil)
		}
	}
	if action == "add" {
		if err = onboardProfile(&cfg, path, o.args, input, out); err != nil {
			return fail(out, errOut, false, err, nil)
		}
	}
	active := o.profile
	if active == "" {
		active = getenv("TACK_PROFILE")
	}
	if active == "" {
		active = cfg.DefaultProfile
	}
	if o.json {
		profiles := map[string]any{}
		for name, p := range cfg.Profiles {
			profiles[name] = map[string]any{"workspace_url": p.WorkspaceURL, "token_env": p.TokenEnv, "cookie_env": p.CookieEnv, "credentials_configured": getenv(p.TokenEnv) != "" && getenv(p.CookieEnv) != ""}
		}
		emitJSON(out, map[string]any{"ok": true, "command": "config", "path": path, "default_profile": cfg.DefaultProfile, "active_profile": active, "profiles": profiles}, nil)
		return 0
	}
	write := func(s string) { emit(out, slack.TerminalSafe(s), nil) }
	write(fmt.Sprintf("Configuration: %s\nDefault profile: %s\n", path, cfg.DefaultProfile))
	if len(cfg.Profiles) == 0 {
		write("No profiles configured. Run tack config add to create one.\n")
		return 0
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := cfg.Profiles[name]
		marker := ""
		if name == active {
			marker = " (active)"
		}
		status := "missing"
		if getenv(p.TokenEnv) != "" && getenv(p.CookieEnv) != "" {
			status = "set (not verified)"
		}
		write(fmt.Sprintf("\n%s%s\n  Workspace: %s\n  Token variable: %s\n  Cookie variable: %s\n  Credentials: %s\n", name, marker, p.WorkspaceURL, p.TokenEnv, p.CookieEnv, status))
	}
	return 0
}

func onboardProfile(cfg *config.Config, path string, args []string, input io.Reader, out io.Writer) error {
	reader := bufio.NewReader(input)
	ask := func(label, fallback string) (string, error) {
		suffix := ""
		if fallback != "" {
			suffix = " [" + fallback + "]"
		}
		emit(out, label+suffix+": ", nil)
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", slack.Usage("profile setup cancelled; configuration unchanged")
		}
		value := strings.TrimSpace(line)
		if value == "" {
			value = fallback
		}
		return value, nil
	}
	emit(out, "Add a Slack profile. Enter environment variable names, not credential values.\n", nil)
	name := ""
	if len(args) > 1 {
		name = args[1]
	}
	var err error
	if name == "" {
		name, err = ask("Profile name", "")
		if err != nil {
			return err
		}
	}
	if !config.ValidName(name) {
		return slack.Usage("invalid profile name; use letters, digits, underscores, or hyphens")
	}
	if _, ok := cfg.Profiles[name]; ok {
		return slack.Usage("profile already exists; edit TOML to change it")
	}
	url, err := ask("Workspace URL (https://example.slack.com)", "")
	if err != nil {
		return err
	}
	stem := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	token, err := ask("Token environment variable", "TACK_"+stem+"_XOXC_TOKEN")
	if err != nil {
		return err
	}
	cookie, err := ask("Cookie environment variable", "TACK_"+stem+"_XOXD_TOKEN")
	if err != nil {
		return err
	}
	profile := config.Profile{WorkspaceURL: url, TokenEnv: token, CookieEnv: cookie}
	if err = config.ValidateProfile(profile); err != nil {
		return err
	}
	fallback := "n"
	if cfg.DefaultProfile == "" {
		fallback = "y"
	}
	choice, err := ask("Set as default? (y/n)", fallback)
	if err != nil {
		return err
	}
	choice = strings.ToLower(choice)
	if choice != "y" && choice != "yes" && choice != "n" && choice != "no" {
		return slack.Usage("answer y or n; configuration unchanged")
	}
	emit(out, slack.TerminalSafe(fmt.Sprintf("Profile: %s\nWorkspace: %s\nToken variable: %s\nCookie variable: %s\n", name, url, token, cookie)), nil)
	confirm, err := ask("Save profile? (y/n)", "y")
	if err != nil {
		return err
	}
	if confirm != "y" && confirm != "yes" {
		return slack.Usage("profile setup cancelled; configuration unchanged")
	}
	cfg.Profiles[name] = profile
	if choice == "y" || choice == "yes" {
		cfg.DefaultProfile = name
	}
	if err = config.Save(path, *cfg); err != nil {
		return err
	}
	emit(out, "Saved profile. Export its credential variables, then verify with:\n  tack whoami --profile "+name+" --json\n", nil)
	return nil
}
