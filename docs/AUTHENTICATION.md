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

## Network boundaries

`tack` accepts only `https://<workspace>.slack.com` or `https://<workspace>.slack-gov.com`.
It rejects URL credentials, custom ports, paths, query strings, and redirects.
It preserves the cookie's encoding and sends form requests to `/api/<read-method>`.
There is no telemetry. Debug output contains method names, parameter names, HTTP status, and duration.
The client honors `Retry-After` and makes no more than three retries per API call.

Browser session support can change. A successful unit test does not prove live session compatibility.
