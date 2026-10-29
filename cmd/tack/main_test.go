package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/javiercm1410/tack/internal/slack"
)

func TestParsing(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, args := range [][]string{{"--json", "search", "hello", "--in", "#general"}, {"search", "hello", "--in=#general", "--json"}, {"search", "--json", "--", "--literal-query"}} {
		o, err := parse(args, now)
		if err != nil || !o.json || o.command != "search" {
			t.Fatalf("%v: %+v %v", args, o, err)
		}
	}
	for _, args := range [][]string{{"search"}, {"whoami", "extra"}, {"bad"}, {"mentions", "--limit", "201"}, {"search", "x", "--since", "bad"}, {"thread", "bad"}, {"channel", "#general", "--unread"}, {"whoami", "--since", "1d"}, {"mentions", "--since", "today", "--until", "yesterday"}} {
		if _, err := parse(args, now); slack.ExitCode(err) != 1 {
			t.Fatalf("accepted %v: %v", args, err)
		}
	}
	if o, err := parse([]string{"mentions"}, now); err != nil || o.since != "7d" {
		t.Fatal(o, err)
	}
}
func TestJSONErrorsAndHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{{[]string{"whoami", "--json"}, 2}, {[]string{"--json", "mentions", "--bad"}, 1}, {[]string{"thread", "bad", "--json"}, 1}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), tc.args, &out, &stderr, func(string) string { return "" })
		if code != tc.code || !json.Valid(out.Bytes()) || stderr.Len() != 0 {
			t.Fatalf("%d %s %s", code, out.String(), stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--help"}, &out, &stderr, func(string) string { t.Fatal("help read credentials"); return "" }); code != 0 || !strings.Contains(out.String(), "mentions") {
		t.Fatal("help failed")
	}
}
func TestOutputRedaction(t *testing.T) {
	token := "xoxc-" + strings.Repeat("a", 20)
	cookie := "xoxd-" + strings.Repeat("b", 20)
	c := slack.NewClient(slack.Credentials{Token: token, Cookie: cookie}, nil)
	var out bytes.Buffer
	emitJSON(&out, map[string]any{"text": token + " " + cookie}, c)
	if !json.Valid(out.Bytes()) || strings.Contains(out.String(), token) || strings.Contains(out.String(), cookie) {
		t.Fatal("JSON leaked credentials")
	}
	out.Reset()
	fail(&out, &bytes.Buffer{}, true, &slack.Error{Kind: "test", Message: token, Code: 5}, c)
	if !json.Valid(out.Bytes()) || strings.Contains(out.String(), token) {
		t.Fatal("JSON error leaked credentials")
	}
	out.Reset()
	m := []message{{TS: "1700000000.000000", Time: "2023-11-14T22:13:20Z", Text: "hello\x1b[2J\n" + strings.Repeat("word ", 100), Where: "#general", UserName: "alex"}}
	render(&out, result{Messages: &m}, options{}, c)
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), strings.Repeat("word ", 99)) {
		t.Fatal("pipe output escaped or truncated incorrectly")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, overrides map[string]string) (*slack.Client, *slack.Resolver, *[]string) {
	t.Helper()
	calls := []string{}
	responses := map[string]string{
		"auth.test":             `{"ok":true,"user":"alex","user_id":"U123","team":"Example","team_id":"T123","url":"https://example.slack.com"}`,
		"users.info":            `{"ok":true,"user":{"id":"U123","name":"alex"}}`,
		"users.list":            `{"ok":true,"members":[{"id":"U123","name":"alex"}]}`,
		"users.lookupByEmail":   `{"ok":true,"user":{"id":"U123","name":"alex"}}`,
		"conversations.info":    `{"ok":true,"channel":{"id":"C123","name":"general","unread_count_display":1}}`,
		"users.conversations":   `{"ok":true,"channels":[{"id":"C123","name":"general","user":"U123","is_member":true}]}`,
		"search.messages":       `{"ok":true,"messages":{"matches":[{"ts":"1700000001.000001","user":"U456","text":"hello <@U123>","channel":{"id":"C123","name":"general"}}],"paging":{"pages":1}}}`,
		"conversations.history": `{"ok":true,"messages":[{"ts":"1700000001.000001","user":"U123","text":"new"},{"ts":"1700000000.000001","user":"U123","text":"old"}]}`,
		"conversations.replies": `{"ok":true,"messages":[{"ts":"1700000000.000001","user":"U123","text":"parent"},{"ts":"1700000001.000001","user":"U456","text":"reply","thread_ts":"1700000000.000001"}]}`,
	}
	for k, v := range overrides {
		responses[k] = v
	}
	tr := transportFunc(func(req *http.Request) (*http.Response, error) {
		method := strings.TrimPrefix(req.URL.Path, "/api/")
		calls = append(calls, method)
		body, ok := responses[method]
		if !ok {
			t.Fatalf("unexpected method %s", method)
		}
		rec := httptest.NewRecorder()
		fmt.Fprint(rec, body)
		return rec.Result(), nil
	})
	c := slack.NewClient(slack.Credentials{BaseURL: "https://example.slack.com", Token: "xoxc-" + "fake", Cookie: "xoxd-" + "fake"}, &http.Client{Transport: tr})
	return c, slack.NewResolver(c, "", true), &calls
}
func TestAllCommands(t *testing.T) {
	for _, args := range [][]string{{"whoami"}, {"mentions"}, {"search", "hello"}, {"from", "@me"}, {"channel", "#general"}, {"thread", "C123", "1700000000.000001"}, {"dms"}, {"dm", "@alex"}, {"channels", "general"}, {"user", "alex@example.test"}} {
		t.Run(args[0], func(t *testing.T) {
			c, r, _ := fixture(t, nil)
			o, err := parse(args, slack.Timestamp("1700000002.000000"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := execute(context.Background(), c, r, o)
			if err != nil || !result.OK || result.Count < 1 {
				t.Fatalf("%+v %v", result, err)
			}
			var out bytes.Buffer
			emitJSON(&out, result, c)
			if !json.Valid(out.Bytes()) {
				t.Fatal("invalid JSON")
			}
			if args[0] == "channel" && (*result.Messages)[0].Text != "old" {
				t.Fatal("history is not chronological")
			}
		})
	}
}
func TestUnreadUnavailable(t *testing.T) {
	c, r, _ := fixture(t, map[string]string{"conversations.info": `{"ok":true,"channel":{"id":"D123","is_im":true}}`})
	_, err := execute(context.Background(), c, r, options{command: "dms", limit: 2, unread: true})
	if err == nil || !strings.Contains(err.Error(), "without --unread") {
		t.Fatal(err)
	}
}
func TestMentionParentsAndSelfExclusion(t *testing.T) {
	c, r, _ := fixture(t, map[string]string{"search.messages": `{"ok":true,"messages":{"matches":[{"ts":"1700000001.000001","user":"U123","text":"self","channel":{"id":"C123","name":"general"}},{"ts":"1700000002.000001","user":"U456","text":"reply","thread_ts":"1700000000.000001","channel":{"id":"C123","name":"general"}}],"paging":{"pages":1}}}`})
	res, err := execute(context.Background(), c, r, options{command: "mentions", limit: 20, threads: true})
	if err != nil || res.Count != 1 || (*res.Messages)[0].Parent == nil {
		t.Fatalf("%+v %v", res, err)
	}
}
func TestEmptyMessagesArray(t *testing.T) {
	c, r, _ := fixture(t, map[string]string{"search.messages": `{"ok":true,"messages":{"matches":[],"paging":{"pages":1}}}`})
	res, err := execute(context.Background(), c, r, options{command: "search", args: []string{"none"}, limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	emitJSON(&out, res, c)
	if !strings.Contains(out.String(), `"messages":[]`) {
		t.Fatal(out.String())
	}
}

func TestThreadExpansionUsesLimit(t *testing.T) {
	c, r, calls := fixture(t, map[string]string{"conversations.history": `{"ok":true,"messages":[{"ts":"1700000000.000001","user":"U123","text":"parent","reply_count":1},{"ts":"1699999999.000001","user":"U123","text":"older"}]}`})
	res, err := execute(context.Background(), c, r, options{command: "channel", args: []string{"C123"}, limit: 2, replies: true})
	if err != nil || res.Count != 2 || (*res.Messages)[1].Text != "reply" {
		t.Fatalf("%+v %v", res, err)
	}
	found := false
	for _, method := range *calls {
		if method == "conversations.replies" {
			found = true
		}
	}
	if !found {
		t.Fatal("thread expansion was skipped")
	}
}

func TestCrossWorkspacePermalinkRejected(t *testing.T) {
	c, r, calls := fixture(t, nil)
	_, err := execute(context.Background(), c, r, options{command: "thread", args: []string{"https://other.slack.com/archives/C123/p1700000000123456"}, limit: 20})
	if slack.ExitCode(err) != 1 || len(*calls) != 0 {
		t.Fatalf("%v calls=%v", err, *calls)
	}
}

func TestDMSearchGroupsLatestMessages(t *testing.T) {
	c, r, calls := fixture(t, map[string]string{"search.messages": `{"ok":true,"messages":{"matches":[{"ts":"1700000003.000001","user":"U123","text":"latest","channel":{"id":"D123","is_im":true,"user":"U123"}},{"ts":"1700000002.000001","user":"U123","text":"older","channel":{"id":"D123","is_im":true,"user":"U123"}},{"ts":"1700000001.000001","user":"U123","text":"group","channel":{"id":"G123","is_mpim":true,"name":"group"}}],"paging":{"pages":1}}}`})
	res, err := execute(context.Background(), c, r, options{command: "dms", limit: 5})
	if err != nil || res.Count != 2 || (*res.Messages)[0].Text != "latest" || (*res.Messages)[1].ChannelType != "mpim" {
		t.Fatalf("%+v %v", res, err)
	}
	for _, method := range *calls {
		if method == "users.conversations" || method == "conversations.history" {
			t.Fatal("DM listing used the incomplete conversation sample")
		}
	}
}

func TestProfileFlag(t *testing.T) {
	for _, args := range [][]string{{"--profile", "other", "whoami"}, {"whoami", "--profile=other"}} {
		o, err := parse(args, time.Now())
		if err != nil || o.profile != "other" {
			t.Fatal(o, err)
		}
	}
}
