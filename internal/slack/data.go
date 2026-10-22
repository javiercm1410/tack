package slack

import (
	"context"
	"net/url"
	"strconv"
)

type Identity struct {
	User   string `json:"user"`
	UserID string `json:"user_id"`
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	URL    string `json:"url"`
}
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	TZ       string `json:"tz"`
	Profile  struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Title       string `json:"title"`
		Email       string `json:"email"`
		StatusText  string `json:"status_text"`
		StatusEmoji string `json:"status_emoji"`
	} `json:"profile"`
}
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	User       string `json:"user"`
	IsIM       bool   `json:"is_im"`
	IsMPIM     bool   `json:"is_mpim"`
	IsMember   bool   `json:"is_member"`
	NumMembers int    `json:"num_members"`
	Unread     *int   `json:"unread_count_display,omitempty"`
	Topic      struct {
		Value string `json:"value"`
	} `json:"topic"`
}

func (c Channel) Kind() string {
	if c.IsIM {
		return "im"
	}
	if c.IsMPIM {
		return "mpim"
	}
	return "channel"
}

type Message struct {
	TS         string  `json:"ts"`
	User       string  `json:"user"`
	Text       string  `json:"text"`
	ThreadTS   string  `json:"thread_ts"`
	ReplyCount int     `json:"reply_count"`
	Permalink  string  `json:"permalink"`
	Channel    Channel `json:"channel"`
}
type metadata struct {
	Next string `json:"next_cursor"`
}

func (c *Client) Whoami(ctx context.Context) (Identity, error) {
	var a Identity
	err := c.Call(ctx, "auth.test", nil, &a)
	return a, err
}
func (c *Client) User(ctx context.Context, id string) (User, error) {
	var r struct {
		User User `json:"user"`
	}
	err := c.Call(ctx, "users.info", url.Values{"user": {id}}, &r)
	return r.User, err
}
func (c *Client) Channel(ctx context.Context, id string) (Channel, error) {
	var r struct {
		Channel Channel `json:"channel"`
	}
	err := c.Call(ctx, "conversations.info", url.Values{"channel": {id}}, &r)
	return r.Channel, err
}

// Walk pages until the visitor has enough results. Reject repeated cursors.
func (c *Client) Channels(ctx context.Context, types string, visit func(Channel) bool) error {
	cursor := ""
	seen := map[string]bool{}
	for {
		var r struct {
			Channels []Channel `json:"channels"`
			Meta     metadata  `json:"response_metadata"`
		}
		if err := c.Call(ctx, "users.conversations", url.Values{"types": {types}, "limit": {"200"}, "cursor": {cursor}, "exclude_archived": {"true"}}, &r); err != nil {
			return err
		}
		for _, ch := range r.Channels {
			if !visit(ch) {
				return nil
			}
		}
		cursor = r.Meta.Next
		if cursor == "" {
			return nil
		}
		if seen[cursor] {
			return &Error{"pagination_error", "Slack returned a repeated cursor", 5}
		}
		seen[cursor] = true
	}
}
func (c *Client) History(ctx context.Context, channel, root, oldest, latest string, limit int) ([]Message, error) {
	method := "conversations.history"
	p := url.Values{"channel": {channel}, "inclusive": {"true"}}
	if root != "" {
		method = "conversations.replies"
		p.Set("ts", root)
	}
	if oldest != "" {
		p.Set("oldest", oldest)
	}
	if latest != "" {
		p.Set("latest", latest)
	}
	result := []Message{}
	seen := map[string]bool{}
	for len(result) < limit {
		p.Set("limit", strconv.Itoa(min(200, limit-len(result))))
		var r struct {
			Messages []Message `json:"messages"`
			Meta     metadata  `json:"response_metadata"`
		}
		if err := c.Call(ctx, method, p, &r); err != nil {
			return nil, err
		}
		for _, m := range r.Messages {
			m.Channel.ID = channel
			result = append(result, m)
			if len(result) == limit {
				break
			}
		}
		cursor := r.Meta.Next
		if cursor == "" {
			break
		}
		if seen[cursor] {
			return nil, &Error{"pagination_error", "Slack returned a repeated cursor", 5}
		}
		seen[cursor] = true
		p.Set("cursor", cursor)
	}
	return result, nil
}
func (c *Client) Search(ctx context.Context, query string, limit int, accept func(Message) bool) ([]Message, error) {
	result := []Message{}
	for page := 1; ; page++ {
		var r struct {
			Messages struct {
				Matches []Message `json:"matches"`
				Paging  struct {
					Pages int `json:"pages"`
				} `json:"paging"`
			} `json:"messages"`
		}
		if err := c.Call(ctx, "search.messages", url.Values{"query": {query}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "count": {strconv.Itoa(min(100, limit))}, "page": {strconv.Itoa(page)}}, &r); err != nil {
			return nil, err
		}
		for _, m := range r.Messages.Matches {
			// Search omits thread_ts but includes it in reply permalinks.
			if m.ThreadTS == "" && m.Permalink != "" {
				if channel, root, err := ParsePermalink(m.Permalink); err == nil && channel == m.Channel.ID && root != m.TS {
					m.ThreadTS = root
				}
			}
			if accept == nil || accept(m) {
				result = append(result, m)
				if len(result) == limit {
					return result, nil
				}
			}
		}
		if page >= r.Messages.Paging.Pages || len(r.Messages.Matches) == 0 {
			return result, nil
		}
	}
}
