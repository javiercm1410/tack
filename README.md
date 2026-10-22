# tack
Read-only Slack for humans and agents. Charm styles the terminal output; `--json` provides stable machine output.

## Install

Go 1.26.10 or later:
```sh
go install github.com/javiercm1410/tack/cmd/tack@latest
```
Release archives include macOS, Linux, and Windows binaries with SHA-256 checksums.
After the first release, `sh scripts/install.sh v0.1.0` installs a verified binary into `~/.local/bin`.
Build locally with `task build`. Use `go build -tags plain ./cmd/tack` for the standard-library-only renderer.
## Authentication

Set `SLACK_WORKSPACE_URL` to your HTTPS Slack workspace URL. There is no default workspace.
Set `SLACK_XOXC_TOKEN` and `SLACK_XOXD_TOKEN` from the same browser session.
Preserve the cookie encoding. See [authentication setup](docs/AUTHENTICATION.md).

If you store the values in macOS Keychain under service `tack`:
```sh
export SLACK_WORKSPACE_URL='https://example.slack.com'
export SLACK_XOXC_TOKEN="$(security find-generic-password -s tack -a xoxc -w 2>/dev/null)"
export SLACK_XOXD_TOKEN="$(security find-generic-password -s tack -a xoxd -w 2>/dev/null)"
tack whoami
```
Every child process of that shell can read these environment variables.
The macOS interactive `security ... -w` prompt can truncate long values; use `-w "$(pbpaste)"` instead.

## Commands

```sh
tack mentions --since 7d --json
tack search "build failure" --in '#general' --limit 20
tack from @me --limit 1
tack channel '#general' --since 1d --with-replies
tack thread 'https://example.slack.com/archives/C123/p1700000000123456'
tack dms --limit 5
tack dm @handle
tack channels general --json
tack user @handle
```
Run `tack --help` for all flags. Quote channel names in shells.
Exit codes: 0 success, 1 usage, 2 authentication, 3 rate limit, 4 not found, 5 API/network error.
Use `--full` for multiline text, `--no-cache` to bypass disk caching, and `NO_COLOR=1` to disable color.
`TACK_CACHE_DIR` overrides the private, session-isolated name cache. Messages and tokens are never cached.
Copy [skill/SKILL.md](skill/SKILL.md) into your agent's `tack` skill directory.

## Implementation notes

Live checks verified mention search, thread parents, DM search, conversation listing, and email lookup fallback.
DMs use `is:dm` search and return the latest matching message per conversation, including group DMs.
Search permalinks supply thread roots when Slack omits the `thread_ts` field.
`--unread` fails explicitly when Slack omits unread counts. Email lookup falls back to `users.list` for browser sessions.
See [implementation decisions and validation](docs/IMPLEMENTATION.md) for limits and checks.

## Policy and license

Use only your own authorized Slack access under your organization's policy. Browser sessions are sensitive credentials.
This tool cannot post, join, mark messages read, or upload. It never evaluates message text.
[MIT](LICENSE). CI and release configuration are adapted from [cloud-swap](https://github.com/javiercm1410/cloud-swap).
