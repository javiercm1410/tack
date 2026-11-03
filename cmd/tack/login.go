package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/javiercm1410/tack/internal/config"
	"github.com/javiercm1410/tack/internal/slack"
)

// secretReader reads one value, without echo where the build supports it.
type secretReader func() (string, error)

// verifier checks a session with auth.test before anything is saved.
type verifier func(context.Context, slack.Credentials) (slack.Identity, error)

type loginIO struct {
	input      io.Reader
	readSecret secretReader
	verify     verifier
}

func runLogin(ctx context.Context, o options, interactive bool, lio loginIO, out, errOut io.Writer, getenv func(string) string) int {
	if len(o.args) > 1 {
		return fail(out, errOut, o.json, slack.Usage("usage: tack "+commands["login"]), nil)
	}
	if o.json || !interactive {
		return fail(out, errOut, o.json, slack.Usage("login requires an interactive terminal"), nil)
	}
	cfg, path, err := config.Load(getenv, true)
	if err == nil {
		err = config.Validate(cfg)
	}
	if err != nil {
		return fail(out, errOut, false, err, nil)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]config.Profile{}
	}
	reader := bufio.NewReader(lio.input)
	name, err := chooseProfile(&cfg, path, o, reader, out)
	if err != nil {
		return fail(out, errOut, false, err, nil)
	}
	p := cfg.Profiles[name]
	read := lio.readSecret
	if read == nil {
		read = terminalSecret(reader)
	}
	emit(out, slack.TerminalSafe(loginHelp(name, p.WorkspaceURL)), nil)
	token, err := askSecret("Token (starts with xoxc-): ", read, out)
	if err != nil {
		return fail(out, errOut, false, err, nil)
	}
	cookie, err := askSecret("Cookie d (starts with xoxd-): ", read, out)
	if err != nil {
		return fail(out, errOut, false, err, nil)
	}
	values := map[string]string{"SLACK_XOXC_TOKEN": token, "SLACK_XOXD_TOKEN": cookie, "SLACK_WORKSPACE_URL": p.WorkspaceURL}
	creds, warning, err := slack.LoadCredentials(func(k string) string { return values[k] })
	if err != nil {
		return fail(out, errOut, false, err, nil)
	}
	if warning != "" {
		emit(errOut, "warning: "+warning+"\n", nil)
	}
	id, err := lio.verify(ctx, creds)
	if err != nil {
		if slack.ExitCode(err) == 2 {
			err = &slack.Error{Kind: "invalid_auth", Message: "Slack rejected these values; nothing was saved. Copy both again from the same tab.", Code: 2}
		}
		return fail(out, errOut, false, err, nil)
	}
	file := credentialsPath(path)
	if err = saveCredentials(file, map[string]string{p.TokenEnv: creds.Token, p.CookieEnv: creds.Cookie}); err != nil {
		return fail(out, errOut, false, err, nil)
	}
	msg := fmt.Sprintf("Logged in to %s as %s (profile %s).\nSaved %s and %s to %s.\n\nLoad them into this shell:\n  source %s\n\nLoad them in every new shell (add once to ~/.zshrc or ~/.bashrc):\n  [ -r %s ] && . %s\n",
		id.Team, id.User, name, p.TokenEnv, p.CookieEnv, file, shellQuote(file), shellQuote(file), shellQuote(file))
	if (getenv(p.TokenEnv) != "" && getenv(p.TokenEnv) != creds.Token) || (getenv(p.CookieEnv) != "" && getenv(p.CookieEnv) != creds.Cookie) {
		msg += "\nThis shell still exports the old values. Run the source command above before you use tack.\n"
	}
	emit(out, slack.TerminalSafe(msg), nil)
	return 0
}

// chooseProfile selects flag/argument > prompt (default profile first) and creates a missing profile.
func chooseProfile(cfg *config.Config, path string, o options, reader *bufio.Reader, out io.Writer) (string, error) {
	name := o.profile
	if len(o.args) == 1 {
		name = o.args[0]
	}
	if name == "" && len(cfg.Profiles) > 0 {
		names := make([]string, 0, len(cfg.Profiles))
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		fallback := cfg.DefaultProfile
		if fallback == "" {
			fallback = names[0]
		}
		emit(out, slack.TerminalSafe(fmt.Sprintf("Profiles: %s\nProfile [%s]: ", strings.Join(names, ", "), fallback)), nil)
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", slack.Usage("login cancelled; nothing was saved")
		}
		name = strings.TrimSpace(line)
		if name == "" {
			name = fallback
		}
	}
	if name != "" {
		if !config.ValidName(name) {
			return "", slack.Usage("invalid profile name; use letters, digits, underscores, or hyphens")
		}
		if _, ok := cfg.Profiles[name]; ok {
			return name, nil
		}
	}
	emit(out, "No profile with that name exists yet. Create it first.\n", nil)
	args := []string{"add"}
	if name != "" {
		args = append(args, name)
	}
	before := map[string]bool{}
	for n := range cfg.Profiles {
		before[n] = true
	}
	if err := onboardProfile(cfg, path, args, reader, out); err != nil {
		return "", err
	}
	for n := range cfg.Profiles {
		if !before[n] {
			return n, nil
		}
	}
	return "", slack.Usage("profile setup did not create a profile")
}

func loginHelp(name, workspace string) string {
	host := workspace
	if u, err := url.Parse(workspace); err == nil && u.Host != "" {
		host = u.Host
	}
	return fmt.Sprintf(`
Log in profile %s (%s).
Copy both values from the same signed-in browser tab. Do not use a private
window, and do not sign out: Slack then ends the session.

  Token: open app.slack.com, DevTools > Console, run:
    copy(Object.values(JSON.parse(localStorage.localConfig_v2).teams).find(t => t.url.includes('%s')).token)
  Cookie: DevTools > Application > Cookies > https://app.slack.com,
    cookie "d" (not "d-s"). Copy the value as shown, still URL-encoded.

Input is hidden. Paste each value and press Enter.
`, name, workspace, host)
}

func askSecret(prompt string, read secretReader, out io.Writer) (string, error) {
	emit(out, prompt, nil)
	v, err := read()
	emit(out, "\n", nil)
	if err != nil {
		return "", slack.Usage("login cancelled; nothing was saved")
	}
	return strings.TrimSpace(v), nil
}

func credentialsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "credentials.env")
}

// saveCredentials rewrites the export lines for the given variables and keeps
// every other line, so profiles share one file. The file is private (0600).
func saveCredentials(path string, values map[string]string) error {
	if path == "" || path == "credentials.env" {
		return slack.Usage("set HOME, USERPROFILE, XDG_CONFIG_HOME, or TACK_CONFIG to store credentials")
	}
	var kept []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			replaced := false
			for k := range values {
				if strings.HasPrefix(line, "export "+k+"=") {
					replaced = true
				}
			}
			if !replaced && line != "" {
				kept = append(kept, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return slack.Usage("cannot read tack credentials file")
	}
	if len(kept) == 0 {
		kept = append(kept, "# tack session credentials, written by tack login. Keep this file private.")
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// LoadCredentials rejects quotes and whitespace, so single quotes are safe.
		kept = append(kept, "export "+k+"='"+values[k]+"'")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return slack.Usage("cannot create tack configuration directory")
	}
	file, err := os.CreateTemp(dir, ".credentials-*.env")
	if err != nil {
		return slack.Usage("cannot write tack credentials file")
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return slack.Usage("cannot protect tack credentials file")
	}
	if _, err = file.WriteString(strings.Join(kept, "\n") + "\n"); err != nil {
		file.Close()
		return slack.Usage("cannot write tack credentials file")
	}
	if err = file.Close(); err != nil {
		return slack.Usage("cannot close tack credentials file")
	}
	if err = os.Rename(temp, path); err != nil {
		return slack.Usage("cannot replace tack credentials file")
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
