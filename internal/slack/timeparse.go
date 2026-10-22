package slack

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func ParseTime(s string, now time.Time) (time.Time, error) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "today":
		return day, nil
	case "yesterday":
		return day.AddDate(0, 0, -1), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		return t, nil
	}
	if len(s) > 1 {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err == nil && n >= 0 && n <= 365000 {
			switch s[len(s)-1] {
			case 'd':
				return now.AddDate(0, 0, -n), nil
			case 'h':
				return now.Add(-time.Duration(n) * time.Hour), nil
			}
		}
	}
	return time.Time{}, Usage("invalid time; use YYYY-MM-DD, today, yesterday, Nd, or Nh")
}
func SearchAfter(t time.Time) string { return t.AddDate(0, 0, -1).Format("2006-01-02") }

var idPattern = regexp.MustCompile(`^[CDG][A-Z0-9]+$`)
var tsPattern = regexp.MustCompile(`^[0-9]{10,}\.[0-9]{6}$`)
var userIDPattern = regexp.MustCompile(`^[UW][A-Z0-9]+$`)

func ValidChannel(id string) bool { return idPattern.MatchString(id) }
func ValidTS(ts string) bool      { return tsPattern.MatchString(ts) }
func ParsePermalink(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", Usage("invalid Slack permalink")
	}
	if err = ValidateURL(u.Scheme + "://" + u.Host); err != nil || u.User != nil || u.Fragment != "" {
		return "", "", Usage("invalid Slack permalink")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "archives" || !ValidChannel(parts[1]) || !strings.HasPrefix(parts[2], "p") {
		return "", "", Usage("invalid Slack permalink")
	}
	digits := strings.TrimPrefix(parts[2], "p")
	if len(digits) < 16 || strings.Trim(digits, "0123456789") != "" {
		return "", "", Usage("invalid Slack permalink timestamp")
	}
	ts := digits[:len(digits)-6] + "." + digits[len(digits)-6:]
	if root := u.Query().Get("thread_ts"); root != "" {
		ts = root
	}
	if !ValidTS(ts) {
		return "", "", Usage("invalid thread timestamp")
	}
	return parts[1], ts, nil
}
func Timestamp(ts string) time.Time {
	parts := strings.SplitN(ts, ".", 2)
	sec, _ := strconv.ParseInt(parts[0], 10, 64)
	var ns int64
	if len(parts) == 2 {
		ns, _ = strconv.ParseInt((parts[1] + "000000000")[:9], 10, 64)
	}
	return time.Unix(sec, ns)
}
func SlackTS(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) + ".000000" }
