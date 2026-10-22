package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/javiercm1410/tack/internal/slack"
)

func emit(w io.Writer, s string, c *slack.Client) {
	if c != nil {
		s = c.Safe(s)
	} else {
		s = slack.Redact(s)
	}
	fmt.Fprint(w, s)
}
func emitJSON(w io.Writer, v any, c *slack.Client) {
	// Marshal after sanitizing strings so exact secrets containing JSON escapes cannot leak.
	data, err := json.Marshal(v)
	if err != nil {
		emit(w, "{\"ok\":false,\"error\":\"encoding_error\",\"exit_code\":5}\n", c)
		return
	}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return
	}
	var clean func(any) any
	clean = func(v any) any {
		switch x := v.(type) {
		case string:
			if c != nil {
				return c.Safe(x)
			}
			return slack.Redact(x)
		case []any:
			for i := range x {
				x[i] = clean(x[i])
			}
		case map[string]any:
			for k, v := range x {
				x[k] = clean(v)
			}
		}
		return v
	}
	data, _ = json.Marshal(clean(decoded))
	fmt.Fprintln(w, string(data))
}
func render(w io.Writer, res result, o options, c *slack.Client) {
	presentation := newPresentation(w)
	write := func(s string) { emit(w, slack.TerminalSafe(s), c) }
	emit(w, presentation.header(res.Command), c)
	if res.Messages != nil {
		if len(*res.Messages) == 0 {
			write("No messages found.\n")
		}
		var line func(message, string)
		line = func(m message, prefix string) {
			t, _ := time.Parse(time.RFC3339, m.Time)
			body := m.Text
			if !o.full {
				body = strings.Join(strings.Fields(body), " ")
			}
			suffix := ""
			if m.IsThreadReply {
				suffix = "  ↳ thread"
			} else if m.ReplyCount > 0 {
				suffix = fmt.Sprintf("  ↳ %d replies", m.ReplyCount)
			}
			text := slack.TerminalSafe(fmt.Sprintf("%s%s  %-20s  %-14s  %s%s", prefix, t.Format("2006-01-02 15:04"), m.Where, m.UserName, body, suffix))
			if !o.full {
				text = presentation.truncate(text)
			}
			write(text + "\n")
			if m.Parent != nil {
				line(*m.Parent, "  parent: ")
			}
		}
		for _, m := range *res.Messages {
			line(m, "")
		}
	}
	if res.Channels != nil {
		if len(*res.Channels) == 0 {
			write("No channels found.\n")
		}
		for _, ch := range *res.Channels {
			write(fmt.Sprintf("%-14s  #%-24s %4d  %s\n", ch.ID, ch.Name, ch.NumMembers, strings.Join(strings.Fields(ch.Topic.Value), " ")))
		}
	}
	if res.Whoami != nil {
		a := res.Whoami
		write(fmt.Sprintf("@%s (%s)\n%s (%s)\n%s\n", a.User, a.UserID, a.Team, a.TeamID, a.URL))
	}
	if res.User != nil {
		u := res.User
		write(fmt.Sprintf("@%s (%s)\n%s · %s\n%s\n%s\n%s\n%s %s\n", u.Name, u.ID, u.RealName, u.Profile.DisplayName, u.Profile.Title, u.Profile.Email, u.TZ, u.Profile.StatusEmoji, u.Profile.StatusText))
	}
}
