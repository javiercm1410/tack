# tack
Read-only Slack CLI for humans and agents. Use `--json` for stable machine output.

## Install

Go 1.26.10 or later:
```sh
go install github.com/javiercm1410/tack/cmd/tack@latest
```
Release archives include macOS, Linux, and Windows binaries with SHA-256 checksums.
Run `sh scripts/install.sh v0.1.0` from this repository to install the verified release into `~/.local/bin`.
Confirm the installation with `tack --version`.
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

## Workspace profiles

Create `~/.config/tack/config.toml` (or `$XDG_CONFIG_HOME/tack/config.toml`):

```toml
default_profile = "accela"

[profiles.accela]
workspace_url = "https://accela.slack.com"
token_env = "SLACK_XOXC_TOKEN"
cookie_env = "SLACK_XOXD_TOKEN"

[profiles.personal]
workspace_url = "https://example.slack.com"
token_env = "PERSONAL_SLACK_XOXC_TOKEN"
cookie_env = "PERSONAL_SLACK_XOXD_TOKEN"
```

Profiles reference environment variable names. Keep token values in your existing
secret store and export them before use. Config contains no credentials.

```sh
tack mentions --since 7d                 # default profile
tack --profile personal mentions --json
tack whoami --profile personal --json
TACK_PROFILE=personal tack whoami
```

See the [profile setup guide](docs/AUTHENTICATION.md#multiple-workspaces-with-profiles)
for separate Keychain entries, environment exports, and default profile changes.

Selection order: `--profile`, then `TACK_PROFILE`, then `default_profile`.
`TACK_CONFIG` overrides the config file path. Unknown profiles and malformed
configuration return exit 1. Missing profile credentials return exit 2. Selected
profiles never fall back to another profile's credentials. Without a selected or
default profile, the original `SLACK_*` environment authentication still works.
Name caches remain isolated by workspace and session. Help and version need no config.

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
