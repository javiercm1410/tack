package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/javiercm1410/tack/internal/config"
	"github.com/javiercm1410/tack/internal/slack"
)

var version = "dev"

type options struct {
	profile                                                                        string
	command                                                                        string
	args                                                                           []string
	json, debug, noCache, full, threads, broadcast, replies, unread, help, version bool
	limit                                                                          int
	since, until, in, from                                                         string
	oldest, latest                                                                 time.Time
}

var commands = map[string]string{"config": "config [show | add [NAME] | default NAME]", "whoami": "whoami", "mentions": "mentions --since 7d", "search": `search "build failure"`, "from": "from @me", "channel": "channel '#general'", "thread": `thread '<permalink>'`, "dms": "dms --limit 10", "dm": "dm @handle", "channels": "channels general", "user": "user @handle"}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
func parse(args []string, now time.Time) (options, error) {
	o := options{limit: 20}
	fs := flag.NewFlagSet("tack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.profile, "profile", "", "configuration profile")
	fs.BoolVar(&o.json, "json", false, "JSON output")
	fs.BoolVar(&o.debug, "debug", false, "safe request diagnostics")
	fs.BoolVar(&o.noCache, "no-cache", false, "bypass disk cache")
	fs.BoolVar(&o.full, "full", false, "full message text")
	fs.BoolVar(&o.help, "help", false, "show help")
	fs.BoolVar(&o.help, "h", false, "show help")
	fs.BoolVar(&o.version, "version", false, "show version")
	fs.IntVar(&o.limit, "limit", 20, "item limit (1-200)")
	fs.StringVar(&o.since, "since", "", "start date")
	fs.StringVar(&o.until, "until", "", "end date")
	fs.StringVar(&o.in, "in", "", "channel filter")
	fs.StringVar(&o.from, "from", "", "author filter")
	fs.BoolVar(&o.threads, "threads", false, "include mention parents")
	fs.BoolVar(&o.broadcast, "include-broadcast", false, "include channel broadcasts")
	fs.BoolVar(&o.replies, "with-replies", false, "expand up to 10 threads")
	fs.BoolVar(&o.unread, "unread", false, "unread DMs")
	flags, positionals := []string{}, []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positionals = append(positionals, a)
			continue
		}
		name := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)[0]
		f := fs.Lookup(name)
		if f == nil {
			return o, slack.Usage("unknown flag: " + name)
		}
		flags = append(flags, a)
		if _, boolean := f.Value.(interface{ IsBoolFlag() bool }); !boolean && !strings.Contains(a, "=") {
			i++
			if i >= len(args) {
				return o, slack.Usage("missing value for --" + name)
			}
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return o, slack.Usage(err.Error())
	}
	if len(positionals) > 0 {
		o.command = positionals[0]
		o.args = positionals[1:]
	}
	if o.version {
		return o, nil
	}
	if o.command == "" {
		o.help = true
		return o, nil
	}
	if _, ok := commands[o.command]; !ok {
		return o, slack.Usage("unknown command: " + o.command)
	}
	if o.help {
		return o, nil
	}
	if o.limit < 1 || o.limit > 200 {
		return o, slack.Usage("--limit must be between 1 and 200")
	}
	bounds := map[string][2]int{"config": {0, 2}, "whoami": {0, 0}, "mentions": {0, 0}, "search": {1, 1}, "from": {1, 1}, "channel": {1, 1}, "thread": {1, 2}, "dms": {0, 0}, "dm": {1, 1}, "channels": {0, 1}, "user": {1, 1}}[o.command]
	if len(o.args) < bounds[0] || len(o.args) > bounds[1] {
		return o, slack.Usage("usage: tack " + commands[o.command])
	}
	permitted := map[string]string{"since": " mentions search from channel dm dms ", "until": " mentions search from channel dm dms ", "in": " search from ", "from": " search ", "threads": " mentions ", "include-broadcast": " mentions ", "with-replies": " channel ", "unread": " dms "}
	var invalid string
	fs.Visit(func(f *flag.Flag) {
		if list, ok := permitted[f.Name]; ok && !strings.Contains(list, " "+o.command+" ") {
			invalid = f.Name
		}
	})
	if invalid != "" {
		return o, slack.Usage("--" + invalid + " is not supported by " + o.command)
	}
	if o.since == "" {
		switch o.command {
		case "mentions", "from", "dms":
			o.since = "7d"
		case "channel", "dm":
			o.since = "1d"
		}
	}
	var err error
	if o.since != "" {
		o.oldest, err = slack.ParseTime(o.since, now)
		if err != nil {
			return o, err
		}
	}
	if o.until != "" {
		o.latest, err = slack.ParseTime(o.until, now)
		if err != nil {
			return o, err
		}
	}
	if !o.oldest.IsZero() && !o.latest.IsZero() && o.latest.Before(o.oldest) {
		return o, slack.Usage("--until must be at or after --since")
	}
	if o.command == "thread" {
		if len(o.args) == 1 {
			_, _, err = slack.ParsePermalink(o.args[0])
		} else if !slack.ValidChannel(o.args[0]) || !slack.ValidTS(o.args[1]) {
			err = slack.Usage("thread requires a channel ID and timestamp")
		}
	}
	return o, err
}
func jsonRequested(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" || a == "--json=true" || a == "-json" {
			return true
		}
	}
	return false
}
func run(ctx context.Context, args []string, out, errOut io.Writer, getenv func(string) string) int {
	o, err := parse(args, time.Now())
	if err != nil {
		return fail(out, errOut, jsonRequested(args), err, nil)
	}
	if o.help {
		emit(out, help(o.command), nil)
		return 0
	}
	if o.version {
		if o.json {
			emitJSON(out, map[string]any{"ok": true, "version": version}, nil)
		} else {
			emit(out, "tack "+version+"\n", nil)
		}
		return 0
	}
	if o.command == "config" {
		info, statErr := os.Stdin.Stat()
		interactive := statErr == nil && info.Mode()&os.ModeCharDevice != 0
		return runConfig(o, os.Stdin, interactive, out, errOut, getenv)
	}
	profileEnv, err := config.Resolve(getenv, o.profile)
	if err != nil {
		return fail(out, errOut, o.json, err, nil)
	}
	creds, warning, err := slack.LoadCredentials(profileEnv)
	if err != nil {
		return fail(out, errOut, o.json, err, nil)
	}
	client := slack.NewClient(creds, nil)
	client.Version = version
	if o.debug {
		client.Debug = errOut
	}
	if warning != "" {
		emit(errOut, warning+"\n", client)
	}
	resolver := slack.NewResolver(client, getenv("TACK_CACHE_DIR"), o.noCache)
	result, err := execute(ctx, client, resolver, o)
	if err != nil {
		return fail(out, errOut, o.json, err, client)
	}
	if err = resolver.Save(); err != nil {
		emit(errOut, "warning: could not save the name cache\n", client)
	}
	if o.json {
		emitJSON(out, result, client)
	} else {
		render(out, result, o, client)
	}
	return 0
}
func fail(out, errOut io.Writer, jsonMode bool, err error, c *slack.Client) int {
	code := slack.ExitCode(err)
	kind := "api_error"
	var api *slack.Error
	if errors.As(err, &api) {
		kind = api.Kind
	}
	if jsonMode {
		emitJSON(out, map[string]any{"ok": false, "error": kind, "message": err.Error(), "exit_code": code}, c)
	} else {
		emit(errOut, "tack: "+err.Error()+"\n", c)
	}
	return code
}
func help(command string) string {
	s := "tack — read-only Slack for humans and agents\n\n"
	if command != "" {
		s += "Usage: tack " + commands[command] + "\n"
	} else {
		for _, name := range []string{"config", "whoami", "mentions", "search", "from", "channel", "thread", "dms", "dm", "channels", "user"} {
			s += fmt.Sprintf("  %-10s tack %s\n", name, commands[name])
		}
	}
	s += "\nGlobal: --profile NAME --json --limit N (1-200) --debug --no-cache --full --help --version\n"
	if command == "" || command == "mentions" {
		s += "Mentions: --since 7d --until DATE --threads --include-broadcast\n"
	}
	if command == "" || command == "search" || command == "from" {
		s += "Search/from: --since TIME --until TIME --in '#channel'; search: --from @user\n"
	}
	if command == "" || command == "channel" || command == "dm" {
		s += "Channel/dm: --since 1d --until TIME; channel: --with-replies\n"
	}
	if command == "" || command == "dms" {
		s += "DMs: --since 7d --until TIME --unread (requires Slack unread counts)\n"
	}
	return s + "Times: YYYY-MM-DD, today, yesterday, Nd, Nh. Quote #channel in shells.\n"
}
func bound(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return slack.SlackTS(t)
}
func queryTime(o options) map[string]any {
	q := map[string]any{"limit": o.limit}
	if !o.oldest.IsZero() {
		q["since"] = o.oldest.Format(time.RFC3339)
	}
	if !o.latest.IsZero() {
		q["until"] = o.latest.Format(time.RFC3339)
	}
	return q
}
func inWindow(m slack.Message, o options) bool {
	t := slack.Timestamp(m.TS)
	return (o.oldest.IsZero() || !t.Before(o.oldest)) && (o.latest.IsZero() || !t.After(o.latest))
}
