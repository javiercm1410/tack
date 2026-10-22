package slack

import (
	"context"
	"testing"
	"time"
)

func TestTimes(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for input, want := range map[string]string{"7d": "2026-10-14T12:00:00Z", "12h": "2026-10-21T00:00:00Z", "today": "2026-10-21T00:00:00Z", "yesterday": "2026-10-20T00:00:00Z", "2026-10-01": "2026-10-01T00:00:00Z"} {
		got, err := ParseTime(input, now)
		if err != nil || got.Format(time.RFC3339) != want {
			t.Errorf("%s: %v %v", input, got, err)
		}
	}
	for _, bad := range []string{"garbage", "-1d", "999999999999h", "2026-02-30"} {
		if _, err := ParseTime(bad, now); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	date, _ := ParseTime("2026-10-20", now)
	if SearchAfter(date) != "2026-10-19" {
		t.Fatal("exclusive after boundary")
	}
}
func TestPermalinks(t *testing.T) {
	base := "https://example.slack.com/archives/C123/p1700000000123456"
	for input, want := range map[string]string{base: "1700000000.123456", base + "?thread_ts=1699999999.000001": "1699999999.000001"} {
		channel, ts, err := ParsePermalink(input)
		if err != nil || channel != "C123" || ts != want {
			t.Fatalf("%s %s %v", channel, ts, err)
		}
	}
	for _, input := range []string{"bad", "https://evil.test/archives/C123/p1700000000123456", base + "?thread_ts=bad", "https://example.slack.com/archives/C123/pfoo"} {
		if _, _, err := ParsePermalink(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
func TestRendering(t *testing.T) {
	r := NewResolver(NewClient(Credentials{}, nil), "", true)
	r.data.Users["U123"] = nameEntry{Name: "alex", TS: time.Now().Unix()}
	got := r.Render(context.Background(), "<@U123> <#C123|general> <https://example.test|docs> <!here> &amp;")
	if got != "@alex #general docs (https://example.test) @here &" {
		t.Fatal(got)
	}
	if TerminalSafe("hello\x1b[2J\x07") != "hello[2J" {
		t.Fatal("terminal control injection")
	}
}
