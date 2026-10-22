package main

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"github.com/javiercm1410/tack/internal/slack"
)

type result struct {
	OK       bool             `json:"ok"`
	Command  string           `json:"command"`
	Query    map[string]any   `json:"query"`
	Count    int              `json:"count"`
	Messages *[]message       `json:"messages,omitempty"`
	Channels *[]slack.Channel `json:"channels,omitempty"`
	User     *slack.User      `json:"user,omitempty"`
	Whoami   *slack.Identity  `json:"whoami,omitempty"`
}
type message struct {
	TS            string   `json:"ts"`
	Time          string   `json:"time"`
	ChannelID     string   `json:"channel_id"`
	ChannelName   string   `json:"channel_name"`
	ChannelType   string   `json:"channel_type"`
	UserID        string   `json:"user_id"`
	UserName      string   `json:"user_name"`
	Text          string   `json:"text"`
	TextRaw       string   `json:"text_raw"`
	ThreadTS      string   `json:"thread_ts"`
	ReplyCount    int      `json:"reply_count"`
	IsThreadReply bool     `json:"is_thread_reply"`
	Permalink     string   `json:"permalink"`
	Parent        *message `json:"parent,omitempty"`
	Where         string   `json:"-"`
}

func execute(ctx context.Context, c *slack.Client, r *slack.Resolver, o options) (result, error) {
	out := result{OK: true, Command: o.command, Query: queryTime(o)}
	var messages []slack.Message
	var err error
	switch o.command {
	case "whoami":
		a, e := c.Whoami(ctx)
		out.Whoami = &a
		out.Count = 1
		return out, e
	case "user":
		id, e := r.ResolveUser(ctx, o.args[0])
		if e != nil {
			return out, e
		}
		u, e := c.User(ctx, id)
		out.User = &u
		out.Count = 1
		return out, e
	case "channels":
		channels := []slack.Channel{}
		filter := ""
		if len(o.args) > 0 {
			filter = strings.ToLower(o.args[0])
		}
		out.Query["filter"] = filter
		err = c.Channels(ctx, "public_channel,private_channel", func(ch slack.Channel) bool {
			r.RememberChannel(ch)
			if strings.Contains(strings.ToLower(ch.Name), filter) {
				channels = append(channels, ch)
			}
			return len(channels) < o.limit
		})
		out.Channels = &channels
		out.Count = len(channels)
		return out, err
	case "search", "from", "mentions":
		messages, err = search(ctx, c, r, o, &out)
	case "channel", "dm":
		var id string
		if o.command == "channel" {
			id, err = r.ResolveChannel(ctx, o.args[0])
		} else {
			var user string
			user, err = r.ResolveUser(ctx, o.args[0])
			if err == nil {
				err = c.Channels(ctx, "im", func(ch slack.Channel) bool {
					r.RememberChannel(ch)
					if ch.User == user {
						id = ch.ID
						return false
					}
					return true
				})
				if err == nil && id == "" {
					err = slack.APIError("dm "+o.args[0], "channel_not_found")
				}
			}
		}
		if err != nil {
			return out, err
		}
		out.Query["channel"] = id
		messages, err = c.History(ctx, id, "", bound(o.oldest), bound(o.latest), o.limit)
		if err == nil && o.replies {
			messages, err = expand(ctx, c, messages, o)
		}
		sort.SliceStable(messages, func(i, j int) bool { return slack.Timestamp(messages[i].TS).Before(slack.Timestamp(messages[j].TS)) })
	case "thread":
		id, ts := "", ""
		if len(o.args) == 1 {
			id, ts, err = slack.ParsePermalink(o.args[0])
			if err == nil {
				u, _ := url.Parse(o.args[0])
				base, _ := url.Parse(c.BaseURL())
				if u.Host != base.Host {
					return out, slack.Usage("permalink belongs to a different workspace")
				}
			}
		} else {
			id, ts = o.args[0], o.args[1]
		}
		if err != nil {
			return out, err
		}
		out.Query["channel"] = id
		out.Query["thread_ts"] = ts
		messages, err = c.History(ctx, id, ts, "", "", o.limit)
	case "dms":
		messages, err = dms(ctx, c, r, o)
	}
	if err != nil {
		return out, err
	}
	rendered := make([]message, 0, len(messages))
	for _, m := range messages {
		item := makeMessage(ctx, c, r, m)
		if o.threads && item.IsThreadReply {
			parents, e := c.History(ctx, m.Channel.ID, m.ThreadTS, "", "", 1)
			if e != nil {
				return out, e
			}
			if len(parents) > 0 {
				parent := makeMessage(ctx, c, r, parents[0])
				item.Parent = &parent
			}
		}
		rendered = append(rendered, item)
	}
	out.Messages = &rendered
	out.Count = len(rendered)
	return out, nil
}
func search(ctx context.Context, c *slack.Client, r *slack.Resolver, o options, out *result) ([]slack.Message, error) {
	query := ""
	self := ""
	switch o.command {
	case "search":
		query = o.args[0]
	case "from":
		id, err := r.ResolveUser(ctx, o.args[0])
		if err != nil {
			return nil, err
		}
		query = "from:<@" + id + ">"
	case "mentions":
		a, err := c.Whoami(ctx)
		if err != nil {
			return nil, err
		}
		self = a.UserID
		query = "<@" + self + "> -from:<@" + self + ">"
	}
	if o.in != "" {
		id, err := r.ResolveChannel(ctx, o.in)
		if err != nil {
			return nil, err
		}
		query += " in:" + id
	}
	if o.from != "" {
		id, err := r.ResolveUser(ctx, o.from)
		if err != nil {
			return nil, err
		}
		query += " from:<@" + id + ">"
	}
	dates := ""
	if !o.oldest.IsZero() {
		dates += " after:" + slack.SearchAfter(o.oldest)
	}
	if !o.latest.IsZero() {
		dates += " before:" + o.latest.AddDate(0, 0, 1).Format("2006-01-02")
	}
	out.Query["search"] = query + dates
	accept := func(m slack.Message) bool { return inWindow(m, o) && (self == "" || m.User != self) }
	messages, err := c.Search(ctx, query+dates, o.limit, accept)
	if err != nil {
		return nil, err
	}
	if o.broadcast {
		member := map[string]bool{}
		if err = c.Channels(ctx, "public_channel,private_channel", func(ch slack.Channel) bool { member[ch.ID] = true; return true }); err != nil {
			return nil, err
		}
		for _, tag := range []string{"here", "channel", "everyone"} {
			more, e := c.Search(ctx, "<!"+tag+"> -from:<@"+self+">"+dates, o.limit, func(m slack.Message) bool {
				return accept(m) && member[m.Channel.ID] && strings.Contains(m.Text, "<!"+tag+">")
			})
			if e != nil {
				return nil, e
			}
			messages = append(messages, more...)
		}
	}
	seen := map[string]bool{}
	unique := []slack.Message{}
	for _, m := range messages {
		key := m.Channel.ID + ":" + m.TS
		if !seen[key] {
			seen[key] = true
			unique = append(unique, m)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return slack.Timestamp(unique[i].TS).After(slack.Timestamp(unique[j].TS)) })
	if len(unique) > o.limit {
		unique = unique[:o.limit]
	}
	return unique, nil
}
func expand(ctx context.Context, c *slack.Client, messages []slack.Message, o options) ([]slack.Message, error) {
	out := []slack.Message{}
	seen := map[string]bool{}
	threads := 0
	for _, m := range messages {
		if len(out) >= o.limit {
			break
		}
		if !seen[m.TS] {
			out = append(out, m)
			seen[m.TS] = true
		}
		if m.ReplyCount == 0 || threads >= 10 || len(out) >= o.limit {
			continue
		}
		threads++
		replies, err := c.History(ctx, m.Channel.ID, m.TS, bound(o.oldest), bound(o.latest), o.limit-len(out)+1)
		if err != nil {
			return nil, err
		}
		for _, reply := range replies {
			if !seen[reply.TS] && len(out) < o.limit {
				seen[reply.TS] = true
				out = append(out, reply)
			}
		}
	}
	return out, nil
}
func dms(ctx context.Context, c *slack.Client, r *slack.Resolver, o options) ([]slack.Message, error) {
	query := "is:dm"
	if !o.oldest.IsZero() {
		query += " after:" + slack.SearchAfter(o.oldest)
	}
	if !o.latest.IsZero() {
		query += " before:" + o.latest.AddDate(0, 0, 1).Format("2006-01-02")
	}
	seen := map[string]bool{}
	messages, err := c.Search(ctx, query, o.limit, func(m slack.Message) bool {
		if !inWindow(m, o) || seen[m.Channel.ID] {
			return false
		}
		seen[m.Channel.ID] = true
		return true
	})
	if err != nil {
		return nil, err
	}
	if !o.unread {
		return messages, nil
	}
	unread := []slack.Message{}
	for _, m := range messages {
		info, err := c.Channel(ctx, m.Channel.ID)
		if err != nil {
			return nil, err
		}
		if info.Unread == nil {
			return nil, &slack.Error{Kind: "unsupported_unread", Message: "Slack did not return unread counts; retry without --unread", Code: 5}
		}
		r.RememberChannel(info)
		if *info.Unread > 0 {
			unread = append(unread, m)
		}
	}
	return unread, nil
}
func makeMessage(ctx context.Context, c *slack.Client, r *slack.Resolver, m slack.Message) message {
	ch := m.Channel
	if (ch.Name == "" && !ch.IsIM && !ch.IsMPIM) || (ch.IsIM && ch.User == "") {
		ch = r.Channel(ctx, ch.ID)
	} else {
		r.RememberChannel(ch)
	}
	permalink := m.Permalink
	if permalink == "" {
		permalink = c.BaseURL() + "/archives/" + ch.ID + "/p" + strings.ReplaceAll(m.TS, ".", "")
	}
	return message{TS: m.TS, Time: slack.Timestamp(m.TS).Local().Format("2006-01-02T15:04:05Z07:00"), ChannelID: ch.ID, ChannelName: ch.Name, ChannelType: ch.Kind(), UserID: m.User, UserName: r.UserName(ctx, m.User), Text: r.Render(ctx, m.Text), TextRaw: m.Text, ThreadTS: m.ThreadTS, ReplyCount: m.ReplyCount, IsThreadReply: m.ThreadTS != "" && m.ThreadTS != m.TS, Permalink: permalink, Where: r.DisplayChannel(ctx, ch)}
}
