package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrForbiddenMethod = errors.New("Slack method is not read-only")

// A switch keeps the allowlist immutable, including from other package code.
func allowed(method string) bool {
	switch method {
	case "auth.test", "search.messages", "conversations.history", "conversations.replies", "conversations.info", "conversations.list", "users.conversations", "users.info", "users.list", "users.lookupByEmail", "team.info":
		return true
	}
	return false
}

type Error struct {
	Kind, Message string
	Code          int
}

func (e *Error) Error() string { return Redact(e.Message) }
func Usage(s string) error     { return &Error{"usage", s, 1} }
func APIError(method, kind string) *Error {
	code, msg := 5, method+": "+kind
	switch kind {
	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		code, msg = 2, "Slack rejected the session. Re-collect both values (README Authentication)."
	case "ratelimited":
		code, msg = 3, "Rate limited by Slack."
	case "channel_not_found", "user_not_found", "thread_not_found", "message_not_found":
		code = 4
	}
	return &Error{kind, msg, code}
}
func ExitCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 5
}

var tokenPattern = regexp.MustCompile(`xox[a-z]-[A-Za-z0-9%._\-]+`)

func Redact(s string) string { return tokenPattern.ReplaceAllString(s, "xox?-REDACTED") }

type Client struct {
	creds   Credentials
	http    *http.Client
	Version string
	Debug   io.Writer
	Sleep   func(context.Context, time.Duration) error
}

func NewClient(creds Credentials, transport *http.Client) *Client {
	if transport == nil {
		transport = &http.Client{Timeout: 20 * time.Second}
	}
	h := *transport
	// Never forward session credentials through redirects, even within Slack.
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if h.Timeout == 0 {
		h.Timeout = 20 * time.Second
	}
	return &Client{creds: creds, http: &h, Version: "dev", Sleep: func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}}
}
func (c *Client) BaseURL() string { return c.creds.BaseURL }
func (c *Client) Safe(s string) string {
	for _, v := range []string{c.creds.Token, c.creds.Cookie} {
		if v != "" {
			s = strings.ReplaceAll(s, v, "REDACTED")
		}
	}
	return Redact(s)
}
func (c *Client) Call(ctx context.Context, method string, params url.Values, out any) error {
	if !allowed(method) {
		return ErrForbiddenMethod
	}
	form := url.Values{}
	names := []string{}
	for k, v := range params {
		if k != "token" {
			form[k] = append([]string(nil), v...)
			names = append(names, Redact(k))
		}
	}
	sort.Strings(names)
	form.Set("token", c.creds.Token)
	for attempt := 0; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.creds.BaseURL+"/api/"+method, strings.NewReader(form.Encode()))
		if err != nil {
			return &Error{"request_error", "could not create Slack request", 5}
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Cookie", "d="+c.creds.Cookie)
		req.Header.Set("User-Agent", "tack/"+c.Version)
		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			return &Error{"network_error", c.Safe(err.Error()), 5}
		}
		if c.Debug != nil {
			fmt.Fprintln(c.Debug, c.Safe(fmt.Sprintf("method=%s params=%v status=%d duration=%s", method, names, resp.StatusCode, time.Since(start))))
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		var envelope struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		decodeErr := json.Unmarshal(data, &envelope)
		if resp.StatusCode == 429 || (decodeErr == nil && envelope.Error == "ratelimited") {
			delay := retryDelay(resp.Header.Get("Retry-After"))
			if attempt == 3 {
				return &Error{"ratelimited", fmt.Sprintf("Rate limited by Slack; retry after %g s.", delay.Seconds()), 3}
			}
			if err := c.Sleep(ctx, delay); err != nil {
				return &Error{"canceled", "request canceled during rate-limit wait", 5}
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &Error{"http_error", fmt.Sprintf("%s: HTTP %d", method, resp.StatusCode), 5}
		}
		if readErr != nil || decodeErr != nil {
			return &Error{"invalid_response", method + ": invalid JSON response", 5}
		}
		if !envelope.OK {
			e := APIError(method, envelope.Error)
			e.Message = c.Safe(e.Message)
			e.Kind = c.Safe(e.Kind)
			return e
		}
		if out != nil && json.Unmarshal(data, out) != nil {
			return &Error{"invalid_response", method + ": unexpected response shape", 5}
		}
		return nil
	}
	return nil
}
func retryDelay(raw string) time.Duration {
	if n, err := strconv.ParseInt(raw, 10, 32); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if date, err := http.ParseTime(raw); err == nil {
		return max(0, time.Until(date))
	}
	return 5 * time.Second
}
