package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fake(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h(w, r)
		return w.Result(), nil
	})
	return NewClient(Credentials{Token: "xoxc-" + "fake", Cookie: "xoxd-" + "fake%2F%3D", BaseURL: "https://example.slack.com"}, &http.Client{Transport: transport})
}
func TestAllowlist(t *testing.T) {
	hits := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { hits++ })
	for _, method := range []string{"chat.postMessage", "chat.update", "chat.delete", "reactions.add", "conversations.mark", "conversations.join", "conversations.open", "files.upload", "AUTH.TEST", "../auth.test"} {
		if err := c.Call(context.Background(), method, nil, nil); !errors.Is(err, ErrForbiddenMethod) {
			t.Errorf("%s: %v", method, err)
		}
	}
	if hits != 0 {
		t.Fatalf("forbidden requests sent: %d", hits)
	}
}
func TestTransportAndRetry(t *testing.T) {
	hits := 0
	var sleeps []time.Duration
	var debug bytes.Buffer
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != "POST" || r.Header.Get("Cookie") != "d=xoxd-"+"fake%2F%3D" {
			t.Error("invalid request or altered cookie")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("token") != "xoxc-"+"fake" {
			t.Error("missing token")
		}
		if hits < 3 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	c.Debug = &debug
	c.Sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }
	p := url.Values{"query": {"xoxc-" + "secret"}}
	if err := c.Call(context.Background(), "search.messages", p, nil); err != nil {
		t.Fatal(err)
	}
	if hits != 3 || len(sleeps) != 2 || sleeps[0] != 2*time.Second {
		t.Fatalf("retry: hits=%d delays=%v", hits, sleeps)
	}
	if strings.Contains(debug.String(), "secret") || strings.Contains(debug.String(), "fake") || p.Get("token") != "" {
		t.Fatal("secret in debug or mutated parameters")
	}
}
func TestRetryExhaustion(t *testing.T) {
	for _, httpLimit := range []bool{true, false} {
		t.Run(fmt.Sprint(httpLimit), func(t *testing.T) {
			hits, sleeps := 0, 0
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				hits++
				w.Header().Set("Retry-After", "0")
				if httpLimit {
					w.WriteHeader(429)
				} else {
					fmt.Fprint(w, `{"ok":false,"error":"ratelimited"}`)
				}
			})
			c.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
			err := c.Call(context.Background(), "auth.test", nil, nil)
			if ExitCode(err) != 3 || hits != 4 || sleeps != 3 {
				t.Fatalf("%v hits %d sleeps %d", err, hits, sleeps)
			}
		})
	}
}
func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	hits := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "example.slack.com" {
			hits++
		}
		http.Redirect(w, r, "https://elsewhere.slack.com", http.StatusTemporaryRedirect)
	})
	if err := c.Call(context.Background(), "auth.test", nil, nil); err == nil || hits != 0 {
		t.Fatalf("redirect followed: %v / %d", err, hits)
	}
}
func TestErrorMappingAndRedaction(t *testing.T) {
	cases := map[string]int{"invalid_auth": 2, "not_authed": 2, "token_revoked": 2, "account_inactive": 2, "ratelimited": 3, "channel_not_found": 4, "user_not_found": 4, "thread_not_found": 4, "message_not_found": 4, "missing_scope": 5, "not_allowed_token_type": 5, "unknown": 5}
	for kind, want := range cases {
		if got := ExitCode(APIError("auth.test", kind)); got != want {
			t.Errorf("%s: %d", kind, got)
		}
	}
	token := "xoxc-" + strings.Repeat("a", 20)
	cookie := "xoxd-" + strings.Repeat("b", 20) + "%2F"
	if s := Redact("error " + token + " " + cookie); strings.Contains(s, token) || strings.Contains(s, cookie) {
		t.Fatal("redaction failed")
	}
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"ok":false,"error":%q}`, token) })
	if err := c.Call(context.Background(), "auth.test", nil, nil); strings.Contains(err.Error(), token) {
		t.Fatal("API error leaked token")
	}
}
func TestCredentials(t *testing.T) {
	env := map[string]string{"SLACK_XOXC_TOKEN": "xoxc-" + "fake", "SLACK_XOXD_TOKEN": "xoxd-" + "fake%2F", "SLACK_WORKSPACE_URL": "https://example.slack.com"}
	get := func(k string) string { return env[k] }
	if _, warning, err := LoadCredentials(get); err != nil || warning == "" {
		t.Fatalf("short cookie: %s / %v", warning, err)
	}
	for _, value := range []string{"", "bad", "xoxc-fake\n", "xoxc-fake\"", "xoxc-fake;other=value"} {
		env["SLACK_XOXC_TOKEN"] = value
		if _, _, err := LoadCredentials(get); ExitCode(err) != 2 {
			t.Fatalf("invalid token accepted: %v", err)
		}
	}
	for _, raw := range []string{"http://example.slack.com", "https://example.slack.com.evil.test", "https://evil.test", "https://example.slack.com:443", "https://user@example.slack.com", "https://example.slack.com/path", "https://example.slack.com?x=1", "https://slack.com", "https://a.b.slack.com"} {
		if ValidateURL(raw) == nil {
			t.Errorf("URL accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://example.slack.com", "https://example.slack-gov.com/"} {
		if err := ValidateURL(raw); err != nil {
			t.Error(err)
		}
	}
}
func TestHistoryPagination(t *testing.T) {
	hits := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		r.ParseForm()
		if hits == 1 {
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"1700000000.000001"}],"response_metadata":{"next_cursor":"next"}}`)
		} else {
			if r.Form.Get("cursor") != "next" {
				t.Error("missing cursor")
			}
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"1700000000.000002"}]}`)
		}
	})
	got, err := c.History(context.Background(), "C123", "1700000000.000000", "", "", 2)
	if err != nil || len(got) != 2 || hits != 2 {
		t.Fatalf("%v / %v / %d", got, err, hits)
	}
}
func TestSearchPaginationAfterFilter(t *testing.T) {
	hits := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		r.ParseForm()
		if r.Form.Get("page") != fmt.Sprint(hits) {
			t.Error("wrong page")
		}
		fmt.Fprintf(w, `{"ok":true,"messages":{"matches":[{"user":"U%d"}],"paging":{"pages":2}}}`, hits)
	})
	got, err := c.Search(context.Background(), "query", 1, func(m Message) bool { return m.User == "U2" })
	if err != nil || len(got) != 1 || hits != 2 {
		t.Fatalf("%v %v", got, err)
	}
}
func TestNameCache(t *testing.T) {
	hits := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"ok":true,"user":{"id":"U123","name":"alex","profile":{"email":"private@example.test"}}}`)
	})
	dir := t.TempDir()
	r := NewResolver(c, dir, false)
	if r.UserName(context.Background(), "U123") != "alex" {
		t.Fatal("name lookup")
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(r.path)
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "fake") {
		t.Fatal("cache includes private profile or credentials")
	}
	info, _ := os.Stat(r.path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("file permissions")
	}
	info, _ = os.Stat(filepath.Dir(r.path))
	if info.Mode().Perm() != 0700 {
		t.Fatal("directory permissions")
	}
	again := NewResolver(c, dir, false)
	again.UserName(context.Background(), "U123")
	if hits != 1 {
		t.Fatal("cache miss")
	}
	bypass := NewResolver(c, dir, true)
	bypass.UserName(context.Background(), "U123")
	if hits != 2 || bypass.path != "" {
		t.Fatal("no-cache ignored")
	}
	expired := again.data.Users["U123"]
	expired.TS = time.Now().Add(-25 * time.Hour).Unix()
	again.data.Users["U123"] = expired
	again.UserName(context.Background(), "U123")
	if hits != 3 {
		t.Fatal("expired cache used")
	}
	other := *c
	other.creds.Token = "another-session"
	if NewResolver(&other, dir, false).path == r.path {
		t.Fatal("cache crosses sessions")
	}
}

func TestSearchRecoversThreadRoot(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"messages":{"matches":[{"ts":"1700000001.000001","channel":{"id":"C123"},"permalink":"https://example.slack.com/archives/C123/p1700000001000001?thread_ts=1700000000.000001"}],"paging":{"pages":1}}}`)
	})
	messages, err := c.Search(context.Background(), "mention", 1, nil)
	if err != nil || len(messages) != 1 || messages[0].ThreadTS != "1700000000.000001" {
		t.Fatalf("%+v %v", messages, err)
	}
}

func TestEmailFallbackPaginatesWithoutCachingEmail(t *testing.T) {
	pages := 0
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "users.lookupByEmail") {
			fmt.Fprint(w, `{"ok":false,"error":"not_allowed_token_type"}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "users.list") {
			t.Fatal("unexpected endpoint")
		}
		pages++
		r.ParseForm()
		if pages == 1 {
			fmt.Fprint(w, `{"ok":true,"members":[],"response_metadata":{"next_cursor":"next"}}`)
			return
		}
		if r.Form.Get("cursor") != "next" {
			t.Error("cursor missing")
		}
		fmt.Fprint(w, `{"ok":true,"members":[{"id":"U123","name":"alex","profile":{"email":"alex@example.test"}}]}`)
	})
	resolver := NewResolver(c, t.TempDir(), false)
	id, err := resolver.ResolveUser(context.Background(), "Alex@example.test")
	if err != nil || id != "U123" || pages != 2 {
		t.Fatalf("%s %v pages=%d", id, err, pages)
	}
	if err = resolver.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(resolver.path)
	if strings.Contains(string(data), "example.test") {
		t.Fatal("profile email persisted")
	}
}
