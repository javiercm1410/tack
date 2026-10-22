package slack

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type nameEntry struct {
	Name     string `json:"name"`
	RealName string `json:"real_name,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Peer     string `json:"peer,omitempty"`
	Members  int    `json:"members,omitempty"`
	TS       int64  `json:"ts"`
}
type cache struct {
	Users    map[string]nameEntry `json:"users"`
	Channels map[string]nameEntry `json:"channels"`
}
type Resolver struct {
	Client *Client
	data   cache
	path   string
	dirty  bool
}

func NewResolver(c *Client, dir string, noCache bool) *Resolver {
	r := &Resolver{Client: c, data: cache{map[string]nameEntry{}, map[string]nameEntry{}}}
	if noCache {
		return r
	}
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return r
		}
		dir = filepath.Join(base, "tack")
	}
	// Isolate caches across workspaces and sessions without storing credentials.
	sum := sha256.Sum256([]byte(c.creds.BaseURL + "\x00" + c.creds.Token))
	r.path = filepath.Join(dir, fmt.Sprintf("%x", sum[:12]), "names.json")
	data, err := os.ReadFile(r.path)
	if err == nil {
		var saved cache
		if json.Unmarshal(data, &saved) == nil && saved.Users != nil && saved.Channels != nil {
			r.data = saved
		}
	}
	return r
}
func fresh(e nameEntry) bool {
	age := time.Now().Unix() - e.TS
	return e.TS > 0 && age >= 0 && age < 86400
}
func (r *Resolver) Save() error {
	if r.path == "" || !r.dirty {
		return nil
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(r.data)
	if err != nil {
		return err
	}
	// Redact again at the disk boundary; only name fields are persisted.
	data = []byte(r.Client.Safe(string(data)))
	f, err := os.CreateTemp(dir, ".names-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, r.path)
}
func (r *Resolver) rememberUser(u User) {
	r.data.Users[u.ID] = nameEntry{Name: u.Name, RealName: u.RealName, TS: time.Now().Unix()}
	r.dirty = true
}
func (r *Resolver) RememberChannel(c Channel) {
	r.data.Channels[c.ID] = nameEntry{Name: c.Name, Kind: c.Kind(), Peer: c.User, Members: c.NumMembers, TS: time.Now().Unix()}
	r.dirty = true
}
func (r *Resolver) UserName(ctx context.Context, id string) string {
	if id == "" {
		return "bot"
	}
	if e, ok := r.data.Users[id]; ok && fresh(e) {
		return e.Name
	}
	u, err := r.Client.User(ctx, id)
	if err != nil {
		return id
	}
	r.rememberUser(u)
	return u.Name
}
func (r *Resolver) Channel(ctx context.Context, id string) Channel {
	if e, ok := r.data.Channels[id]; ok && fresh(e) && !(e.Kind == "im" && e.Peer == "") {
		return Channel{ID: id, Name: e.Name, IsIM: e.Kind == "im", IsMPIM: e.Kind == "mpim", User: e.Peer, NumMembers: e.Members}
	}
	c, err := r.Client.Channel(ctx, id)
	if err != nil {
		return Channel{ID: id, Name: id}
	}
	r.RememberChannel(c)
	return c
}
func (r *Resolver) DisplayChannel(ctx context.Context, c Channel) string {
	if c.IsIM {
		return "DM @" + r.UserName(ctx, c.User)
	}
	if c.IsMPIM {
		if c.NumMembers > 0 {
			return fmt.Sprintf("DM group (%d)", c.NumMembers)
		}
		return "DM group"
	}
	return "#" + c.Name
}
func (r *Resolver) ResolveUser(ctx context.Context, input string) (string, error) {
	name := strings.TrimPrefix(input, "@")
	if name == "me" {
		a, err := r.Client.Whoami(ctx)
		return a.UserID, err
	}
	if userIDPattern.MatchString(name) {
		return name, nil
	}
	if strings.Contains(name, "@") {
		var out struct {
			User User `json:"user"`
		}
		err := r.Client.Call(ctx, "users.lookupByEmail", url.Values{"email": {name}}, &out)
		if err == nil {
			r.rememberUser(out.User)
			return out.User.ID, nil
		}
		var api *Error
		if !errors.As(err, &api) || (api.Kind != "not_allowed_token_type" && api.Kind != "missing_scope") {
			return "", err
		}
		// Browser sessions can reject this method. Page users.list without
		// persisting profile emails in the name cache.
	}
	for id, e := range r.data.Users {
		if fresh(e) && e.Name == name {
			return id, nil
		}
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		var out struct {
			Members []User   `json:"members"`
			Meta    metadata `json:"response_metadata"`
		}
		if err := r.Client.Call(ctx, "users.list", url.Values{"limit": {"200"}, "cursor": {cursor}}, &out); err != nil {
			return "", err
		}
		found := ""
		for _, u := range out.Members {
			r.rememberUser(u)
			if u.Name == name || (strings.Contains(name, "@") && strings.EqualFold(u.Profile.Email, name)) {
				found = u.ID
			}
		}
		if found != "" {
			return found, nil
		}
		cursor = out.Meta.Next
		if cursor == "" {
			break
		}
		if seen[cursor] {
			return "", &Error{"pagination_error", "Slack returned a repeated cursor", 5}
		}
		seen[cursor] = true
	}
	return "", APIError("user "+input, "user_not_found")
}
func (r *Resolver) ResolveChannel(ctx context.Context, input string) (string, error) {
	name := strings.TrimPrefix(input, "#")
	if ValidChannel(name) {
		return name, nil
	}
	for id, e := range r.data.Channels {
		if fresh(e) && e.Name == name {
			return id, nil
		}
	}
	found := ""
	err := r.Client.Channels(ctx, "public_channel,private_channel", func(c Channel) bool {
		r.RememberChannel(c)
		if c.Name == name {
			found = c.ID
			return false
		}
		return true
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", APIError("channel "+input, "channel_not_found")
	}
	return found, nil
}

var markup = regexp.MustCompile(`<([^<>]+)>`)

func (r *Resolver) Render(ctx context.Context, text string) string {
	text = markup.ReplaceAllStringFunc(text, func(s string) string {
		inner := s[1 : len(s)-1]
		parts := strings.SplitN(inner, "|", 2)
		id := parts[0]
		switch {
		case strings.HasPrefix(id, "@"):
			return "@" + r.UserName(ctx, strings.TrimPrefix(id, "@"))
		case strings.HasPrefix(id, "#"):
			if len(parts) == 2 {
				return "#" + parts[1]
			}
			return "#" + r.Channel(ctx, strings.TrimPrefix(id, "#")).Name
		case id == "!here" || id == "!channel" || id == "!everyone":
			return "@" + id[1:]
		case strings.HasPrefix(id, "https://") || strings.HasPrefix(id, "http://"):
			if len(parts) == 2 {
				return parts[1] + " (" + id + ")"
			}
			return id
		}
		return s
	})
	return html.UnescapeString(text)
}

// Strip control characters before terminal display. JSON retains escaped raw data.
func TerminalSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}
