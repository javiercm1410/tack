# Authentication

`tack` uses a browser session, not a Slack application token. Your organization must permit this access.
The CLI reads environment variables only. It does not collect browser data or access Keychain automatically.

1. Sign in to your workspace in the Slack web application.
2. Open the browser's developer tools on the Slack application page.
3. Find the selected workspace token in `localStorage.localConfig_v2.teams`.
4. Copy that workspace's token into `SLACK_XOXC_TOKEN` through your secret manager.
5. Find the HttpOnly cookie named `d` for the Slack domain in the browser's cookie storage.
6. Copy the cookie, with its original URL encoding, into `SLACK_XOXD_TOKEN` through your secret manager.
7. Set `SLACK_WORKSPACE_URL` to the workspace's HTTPS URL.
8. Run `tack whoami`.

Both session values must come from the same browser session. Refresh both values after exit code 2.
Do not send tokens in chat, issues, screenshots, shell command arguments to `tack`, or committed files.

## Optional macOS Keychain storage

After you copy each value, these commands store it under a neutral service name:

```sh
security add-generic-password -U -s tack -a xoxc -w "$(pbpaste)"
security add-generic-password -U -s tack -a xoxd -w "$(pbpaste)"
```

Run each command only after you copy the corresponding value. The interactive password prompt can truncate long values.
The explicit `-w` argument avoids that prompt, but the value exists briefly in the `security` process arguments.
A password manager can avoid this command-line storage step. Clear the clipboard after storage.
The README shows how to load the values into a shell without saving them in a shell configuration file.
Every child process of that shell inherits the values. Use a dedicated shell session when practical.

## Multiple workspaces with profiles

Create the configuration directory:

```sh
mkdir -p ~/.config/tack
```

Save this configuration as `~/.config/tack/config.toml`:

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

Replace the second workspace URL with your actual workspace URL.
Each profile references two environment variable names. It contains no token values.
The workspace URL comes from the selected profile, rather than `SLACK_WORKSPACE_URL`.
Use letters, digits, underscores, or hyphens in profile names.

### Preserve your existing workspace

The `accela` profile above uses the original credential variable names.
Keep your current credential exports. Commands without `--profile` use Accela.
If your existing Keychain entries use service `slack-mcp`, keep that service name.
There is no need to copy or replace the existing credentials.

### Configure another workspace

Sign in to the other workspace and collect its session values using the steps above.
Store its credentials under a separate Keychain service, such as `tack-personal`.
After you copy its `xoxc` value, run:

```sh
security add-generic-password -U -s tack-personal -a xoxc -w "$(pbpaste)"
```

After you copy its `d` cookie, run:

```sh
security add-generic-password -U -s tack-personal -a xoxd -w "$(pbpaste)"
pbcopy </dev/null
```

Export the two variable names from the `personal` profile:

```sh
export PERSONAL_SLACK_XOXC_TOKEN="$(security find-generic-password -s tack-personal -a xoxc -w 2>/dev/null)"
export PERSONAL_SLACK_XOXD_TOKEN="$(security find-generic-password -s tack-personal -a xoxd -w 2>/dev/null)"
tack whoami --profile personal --json
```

Add these exports to `~/.zshrc` if each new interactive shell must load them.
Open a new shell after the change. Restart an agent that started before the exports.
A scheduled job must load these variables itself; it does not read `~/.zshrc` automatically.
Other systems can use their existing secret manager to populate the same variables.

### Select and verify a profile

```sh
tack whoami --json                         # configured default
tack --profile personal whoami --json      # explicit profile
tack mentions --profile personal --since 7d --json
TACK_PROFILE=personal tack whoami --json    # override for this process
```

Selection order is `--profile`, `TACK_PROFILE`, then `default_profile`.
Change `default_profile` in the TOML file to change the persistent default.
Unset `TACK_PROFILE` when you want the configured default to apply.
A selected profile never borrows credentials from another profile.

`TACK_CONFIG` overrides the configuration file path.
Otherwise, the path is `$XDG_CONFIG_HOME/tack/config.toml`, or
`~/.config/tack/config.toml` when `XDG_CONFIG_HOME` is unset.
On Windows, `USERPROFILE` supplies the home directory when `HOME` is unset.
Without a selected or default profile, the original three `SLACK_*` variables still work.

Unknown profiles, unknown TOML fields, invalid URLs, and malformed configuration return exit 1.
Missing or expired credentials return exit 2. Refresh only the affected profile's session values.
The name cache remains isolated by workspace URL and session token.

## Network boundaries

`tack` accepts only `https://<workspace>.slack.com` or `https://<workspace>.slack-gov.com`.
It rejects URL credentials, custom ports, paths, query strings, and redirects.
It preserves the cookie's encoding and sends form requests to `/api/<read-method>`.
There is no telemetry. Debug output contains method names, parameter names, HTTP status, and duration.
The client honors `Retry-After` and makes no more than three retries per API call.

Browser session support can change. A successful unit test does not prove live session compatibility.
