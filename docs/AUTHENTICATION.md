# Authentication

`tack` uses a browser session, not a Slack application token. Your organization must permit this access.
At run time, `tack` reads credentials from environment variables only. `tack login` collects the
two session values that you paste, checks them with Slack, and saves them as shell exports.

## Log in

```sh
tack login            # prompt for a profile; Enter selects the default profile
tack login personal   # log in a named profile; create it if it does not exist
```

`tack login` does these steps:

1. It selects or creates the profile. A new profile asks for the workspace URL and variable names.
2. It shows where to find the two values, then reads each value with hidden input.
3. It checks the values with `auth.test`. If Slack rejects them, it saves nothing.
4. It writes `export` lines for the profile's two variables to `credentials.env`.
5. It prints the `source` command that loads the new values into the current shell.

Collect the values from one signed-in browser tab of the Slack web application:

- **Token** (`xoxc-...`): in the developer tools console, run
  `copy(Object.values(JSON.parse(localStorage.localConfig_v2).teams).find(t => t.url.includes('<workspace>.slack.com')).token)`.
- **Cookie** (`xoxd-...`): in the developer tools storage view, open the cookies for
  `https://app.slack.com` and copy cookie `d` (not `d-s`). Keep its URL encoding.

Both values must come from the same browser session. Do not use a private window, and do not
sign out of that tab: Slack then ends the session and `tack` returns exit code 2.
Run `tack login` again after exit code 2.

## Load the credentials

`credentials.env` is next to `config.toml`, normally `~/.config/tack/credentials.env`.
It has private permissions (0600) and holds one `export` line per variable. A new login replaces
only the lines of its own profile. A program cannot change its parent shell, so load the file:

```sh
source ~/.config/tack/credentials.env                                  # current shell
[ -r ~/.config/tack/credentials.env ] && . ~/.config/tack/credentials.env   # add to ~/.zshrc
```

The file always overwrites the variables. After a new login, source it again in every open shell
that still has the old values. Restart an agent that started before the change.
A scheduled job must source the file itself. Every child process of the shell inherits the values.

The file stores the session in plain text. Do not commit it, share it, or put it in a backup that
other people can read. To use a secret manager instead, export the same variable names yourself;
`tack` reads only the environment.
Do not send tokens in chat, issues, screenshots, or shell command arguments to `tack`.

## Multiple workspaces with profiles

### Interactive setup

Run `tack login NAME` to create a profile and log in at the same time. Run `tack config add`
to create a profile without credentials. The setup asks for:

1. A unique profile name.
2. The HTTPS URL of the Slack workspace.
3. The names of the token and cookie environment variables.
4. Whether this profile becomes the default.
5. Confirmation before the file is saved.

Press Enter to accept a displayed default. Enter `n` at the final prompt to cancel.
An interrupted setup leaves the existing file unchanged. Existing profile names
cannot be overwritten by setup. Use a new name or edit the TOML file.
The first profile becomes the suggested default. Additional profiles preserve the
current default unless you select a new default.

```sh
tack config add personal
tack config
tack config show --json
tack config default accela
tack config default accela --json
```

`config.toml` saves environment variable names, not credential values. `tack config`
reports whether both variables are set; it does not verify Slack authentication.
Run `tack whoami --profile personal --json` to verify the configured session.

For automated setup, write the TOML file and use `tack config --json` to inspect it.
Interactive setup and `tack login` reject `--json` and piped input. Configuration changes
preserve other profiles and replace the file atomically with private permissions.
TOML comments and formatting are rewritten.
`TACK_PROFILE` and `--profile` override the saved default for reads; they do not
change which profile `tack config default NAME` saves as the persistent default.

### Manual setup

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
Then run `tack login accela` and `tack login personal`.

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
Missing or expired credentials return exit 2. Run `tack login NAME` for the affected profile.
The name cache remains isolated by workspace URL and session token.

## Network boundaries

`tack` accepts only `https://<workspace>.slack.com` or `https://<workspace>.slack-gov.com`.
It rejects URL credentials, custom ports, paths, query strings, and redirects.
It preserves the cookie's encoding and sends form requests to `/api/<read-method>`.
There is no telemetry. Debug output contains method names, parameter names, HTTP status, and duration.
The client honors `Retry-After` and makes no more than three retries per API call.

Browser session support can change. A successful unit test does not prove live session compatibility.
