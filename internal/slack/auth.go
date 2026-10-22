package slack

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

type Credentials struct{ Token, Cookie, BaseURL string }

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Usage("SLACK_WORKSPACE_URL must be an HTTPS Slack workspace URL")
	}
	host := u.Hostname()
	valid := false
	for _, suffix := range []string{".slack.com", ".slack-gov.com"} {
		if strings.HasSuffix(host, suffix) {
			name := strings.TrimSuffix(host, suffix)
			valid = name != "" && !strings.Contains(name, ".") && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
		}
	}
	if !valid {
		return Usage("SLACK_WORKSPACE_URL must be an HTTPS Slack workspace URL")
	}
	return nil
}

func LoadCredentials(getenv func(string) string) (Credentials, string, error) {
	c := Credentials{getenv("SLACK_XOXC_TOKEN"), getenv("SLACK_XOXD_TOKEN"), strings.TrimRight(getenv("SLACK_WORKSPACE_URL"), "/")}
	if c.Token == "" || c.Cookie == "" {
		return c, "", &Error{"missing_credentials", "missing SLACK_XOXC_TOKEN / SLACK_XOXD_TOKEN; see README Authentication", 2}
	}
	for _, pair := range [][2]string{{c.Token, "xoxc-"}, {c.Cookie, "xoxd-"}} {
		if !strings.HasPrefix(pair[0], pair[1]) || len(pair[0]) <= 5 || strings.ContainsAny(pair[0], "\"';,\\") || strings.ContainsFunc(pair[0], unicode.IsSpace) || strings.ContainsFunc(pair[0], unicode.IsControl) {
			return c, "", &Error{"invalid_credentials", "invalid Slack session format; see README Authentication", 2}
		}
	}
	if err := ValidateURL(c.BaseURL); err != nil {
		return c, "", err
	}
	warning := ""
	if len(c.Cookie) < 150 {
		warning = fmt.Sprintf("SLACK_XOXD_TOKEN looks truncated (%d chars)", len(c.Cookie))
	}
	return c, warning, nil
}
