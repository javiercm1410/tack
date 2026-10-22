# tack implementation

## Public scope

`tack` reads Slack data from one explicitly configured workspace per invocation.
The public repository uses synthetic examples and contains no source workspace details.
The CLI supports `whoami`, `mentions`, `search`, `from`, `channel`, `thread`, `dms`, `dm`, `channels`, and `user`.
It cannot post, edit, delete, react, mark read, join, open conversations, or upload.

## Decisions

- The binary and module are named `tack`.
- The Slack client uses the Go standard library. The default renderer uses Charm Lip Gloss.
- `-tags plain` removes renderer dependencies from the build, while preserving command and JSON behavior.
- The workspace URL is required. No company URL, user, channel, or message is embedded in the tool.
- Credentials come only from environment variables. HTTP redirects are rejected.
- The immutable API allowlist is a switch in the single request function.
- Name cache entries expire after 24 hours. Each session and workspace has a separate cache directory.
- Cache files use mode `0600`; cache directories use `0700`. The cache excludes messages and profile emails.
- Global flags work before or after the command. `--` ends flag parsing for literal queries.
- Search dates include a wider day window, followed by exact timestamp filtering.
- `--until` is an inclusive instant; a calendar date means midnight at the start of that date.
- Threads and expanded channel output respect `--limit`. Thread expansion stops after ten parents.
- `dms` uses `is:dm` search, sorted newest first, and deduplicates by conversation.
- Search reply permalinks provide thread roots when the response omits `thread_ts`.
- Email lookup falls back to paginated `users.list` when the session rejects `users.lookupByEmail`.
- JSON output is one object. Empty message results contain `"messages": []`.
- JSON errors contain `ok`, `error`, `message`, and `exit_code`.
- JSON message fields include timestamps, resolved names, raw text, thread metadata, and a permalink.
- Raw JSON text retains escaped control characters; terminal output removes control characters before display.
- The repository supplies an agent skill. Install it in the target agent's skill directory; installation is separate from the CLI build.

## Live compatibility checks

Private smoke checks ran against an authorized browser session. The repository contains no real session responses.

1. S1: Raw mention search matches direct mentions and thread replies. Reply permalinks expose the thread root.
2. S2: `is:dm` returns both one-to-one and group DMs. The CLI selects the latest match per conversation.
3. S3: The tested session omits `unread_count_display`. `--unread` reports `unsupported_unread` explicitly.
4. S4: `users.conversations` works. The session rejects `users.lookupByEmail`; the CLI falls back to `users.list`.

The implementation uses no undocumented Activity endpoint. It never infers unread counts from missing fields.
Search results depend on Slack indexing and the caller's access. They do not establish that every message was returned.
Name resolution falls back to IDs when an optional lookup fails. Required command requests retain their error exit codes.

## Validation

Run `task check` for formatting, vet, tests, race tests, and the default build.
Run `task build:all` for four static release targets.
Run `task release:check` to validate GoReleaser configuration.
Run `go test -tags plain -race ./...` in an environment without renderer dependencies.
Tests use synthetic data and an in-memory HTTP transport with `httptest.ResponseRecorder`; they need no network listener.
The CI workflow checks both renderers, build targets, dependencies, and release configuration.

After local and CI checks, run `whoami`, `mentions --since 3d`, `from @me --limit 1`, a known thread,
`channels --json`, and `dms --limit 5` with your private session. Repeat these checks when Slack session compatibility changes.
