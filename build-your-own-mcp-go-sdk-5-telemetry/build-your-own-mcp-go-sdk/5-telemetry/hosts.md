# Trace an investigation from a host

Build the server and run it over HTTP with telemetry enabled as [README.md](README.md) describes. Use an unused MCP registration name. These examples use `log-triage-telemetry`, so your normal server registration can remain intact.

## Links

- [Claude Code MCP](https://code.claude.com/docs/en/mcp)
- [Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
- [LangSmith OTLP](https://docs.langchain.com/langsmith/trace-with-opentelemetry)

## Claude Code

Load the example tokens into the terminal that starts the host, then register the running HTTP server:

```bash
# Run from the repository root.
# macOS and Linux
set -a
. 4-agents-and-clients/tokens.example.env
set +a
claude mcp add --scope project --transport http log-triage-telemetry http://127.0.0.1:8765/mcp --header 'Authorization: Bearer ${LOG_TRIAGE_TOKEN}'
claude --tools ""

# Windows (PowerShell)
Get-Content 4-agents-and-clients/tokens.example.env | ForEach-Object { if ($_ -match '^(\w+)=(.*)$') { Set-Item -Path "Env:$($Matches[1])" -Value $Matches[2] } }
claude mcp add --scope project --transport http log-triage-telemetry http://127.0.0.1:8765/mcp --header 'Authorization: Bearer ${LOG_TRIAGE_TOKEN}'
claude --tools ""
```

The single quotes store a variable reference, not the token value, in PowerShell too. `--tools ""` removes Claude Code's built-in tools, not other MCP servers you have added. In the fresh session, inspect `/mcp`, enable the server if needed and ask the investigation question from the lesson. Check each tool call belongs to `log-triage-telemetry`. Approve writes only after reviewing their proposed finding.

## Codex

```bash
# Run from the repository root.
# macOS and Linux
set -a
. 4-agents-and-clients/tokens.example.env
set +a
codex mcp add log-triage-telemetry --url http://127.0.0.1:8765/mcp --bearer-token-env-var LOG_TRIAGE_TOKEN
codex --no-daemon

# Windows (PowerShell)
Get-Content 4-agents-and-clients/tokens.example.env | ForEach-Object { if ($_ -match '^(\w+)=(.*)$') { Set-Item -Path "Env:$($Matches[1])" -Value $Matches[2] } }
codex mcp add log-triage-telemetry --url http://127.0.0.1:8765/mcp --bearer-token-env-var LOG_TRIAGE_TOKEN
codex --no-daemon
```

Codex stores this registration in its user configuration. It usually connects to its servers through a background server that keeps the environment it started with, not this terminal's, so `--no-daemon` starts the session without it and the token comes from this terminal. Inspect `/mcp` in the fresh session and use the lesson's question. The chat instruction to use only this server's tools is a workflow rule, not an operating-system sandbox. Verify the tool transcript before counting the run as MCP-only evidence.

## LangSmith and the local viewer

The host does not start the HTTP server, so telemetry settings belong in the server terminal. For LangSmith, stop the server normally, then create an API key in LangSmith's API Keys settings: keep the type Personal Access Token and pick an expiration.

The key is a password to your LangSmith account, so keep it where passwords live, in your system's credential store: the Keychain on macOS, the desktop keyring on Linux, and on Windows SecretStore, an encrypted vault file for your user that PowerShell manages. A `.env` file would hold the key in plain text, one `git add` away from a commit. Store the key once:

```bash
# Run once, in any terminal.
# macOS: your login Keychain; asks twice and shows nothing
security add-generic-password -a "$USER" -s langsmith-api-key -w

# Linux: your desktop keyring; asks once and shows nothing
secret-tool store --label=langsmith-api-key service langsmith-api-key account "$USER"

# Windows (PowerShell): install PowerShell 7 once, then in a new pwsh window install Microsoft's secret modules, let the vault open without a password and store the key
winget install --id Microsoft.PowerShell --source winget
Install-Module Microsoft.PowerShell.SecretManagement, Microsoft.PowerShell.SecretStore -Scope CurrentUser
Register-SecretVault -Name SecretStore -ModuleName Microsoft.PowerShell.SecretStore -DefaultVault
Set-SecretStoreConfiguration -Authentication None
Set-Secret -Name langsmith-api-key
```

- Linux: `secret-tool` comes in the `libsecret-tools` package on Debian and Ubuntu (`sudo apt install libsecret-tools`) and in `libsecret` on Fedora, and it needs a desktop session running GNOME Keyring or KWallet, unlocked at login.
- Windows: PowerShell 7 (`pwsh`, 7.3 or later) installs next to the Windows PowerShell that ships with Windows, and every Windows line in this practice runs in it. Microsoft now maintains both secret modules for security and critical bug fixes only. If SecretStore already keeps a secret of yours this way, run only the `Set-Secret` line.
- Windows: `Set-SecretStoreConfiguration -Authentication None` asks you to confirm, then creates the encrypted vault file and asks you twice for its new password. After that, no PowerShell session asks for the password again, so a new terminal loads the key with one line. Microsoft's docs advise against this setting for important secrets, and this key is for a free LangSmith project, expires on the date you picked, and goes in the clean-up below. `Set-Secret` then shows only asterisks as you paste.

Check that the key is stored. The check prints `stored` only when the item exists, and never the key:

```bash
# Run in any terminal.
# macOS
security find-generic-password -a "$USER" -s langsmith-api-key >/dev/null && echo stored

# Linux
secret-tool lookup service langsmith-api-key account "$USER" >/dev/null && echo stored

# Windows (PowerShell)
if (Get-SecretInfo -Name langsmith-api-key) { 'stored' }
```

```text
stored
```

In the server terminal, load the key from the store into `LANGSMITH_API_KEY`. The variable lasts as long as the terminal, so a new terminal needs this line again:

```bash
# Run from the repository root, server terminal.
# macOS
export LANGSMITH_API_KEY="$(security find-generic-password -a "$USER" -s langsmith-api-key -w)"

# Linux
export LANGSMITH_API_KEY="$(secret-tool lookup service langsmith-api-key account "$USER")"

# Windows (PowerShell)
$env:LANGSMITH_API_KEY = Get-Secret -Name langsmith-api-key -AsPlainText
```

Then set the exporter variables and restart:

```bash
# Run from the repository root, server terminal.
# macOS and Linux
unset OTEL_EXPORTER_OTLP_TRACES_ENDPOINT OTEL_EXPORTER_OTLP_TRACES_HEADERS
export OTEL_EXPORTER_OTLP_ENDPOINT=https://api.smith.langchain.com/otel
export OTEL_SERVICE_NAME=log-triage
export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=$LANGSMITH_API_KEY,Langsmith-Project=LogTriagePractice"
solution/bin/log-triage --root s3_logs --reports reports --http 127.0.0.1:8765 --log-level debug

# Windows (PowerShell)
Remove-Item Env:OTEL_EXPORTER_OTLP_TRACES_ENDPOINT, Env:OTEL_EXPORTER_OTLP_TRACES_HEADERS -ErrorAction Ignore
$env:OTEL_EXPORTER_OTLP_ENDPOINT = 'https://api.smith.langchain.com/otel'
$env:OTEL_SERVICE_NAME = 'log-triage'
$env:OTEL_EXPORTER_OTLP_HEADERS = "x-api-key=$env:LANGSMITH_API_KEY,Langsmith-Project=LogTriagePractice"
./solution/bin/log-triage.exe --root s3_logs --reports reports --http 127.0.0.1:8765 --log-level debug
```

Use the API host for your account's region. The US base URL above intentionally omits `/v1/traces`. The exporter appends it. The key lives in your credential store and is loaded into the server terminal only: keep it out of source, `.env` files, screenshots and transcripts.

After a successful tool call, open `LogTriagePractice` in LangSmith and inspect its tool run and child spans. LangSmith keeps each span's name, duration and IDs and the service name and version, makes the tool run and its tool name from `gen_ai.tool.name`, and drops the attributes it doesn't map, `error.type` and the `logtriage.*` counters included. A refusal on this server therefore shows as a successful run there. Each Inspector command opens its own session, so `server/discover`, `subscriptions/listen` and `tools/list` appear as runs beside the tool run: filter on run type tool.

For the counters and `error.type`, stop the server normally, start the local receiver from [README.md](README.md), restart the server with its endpoint, and repeat the call: the counters are the same for the same arguments.

If a host supplies a sampled parent, its ancestor spans must reach LangSmith too. A missing ancestor can make child runs disappear after buffering, even when export returned success. Use the local receiver to inspect missing-parent traces, or configure host export to the same backend with its documented settings. Keep the server's propagation behavior unchanged.

## Correct a registration and clean up

If you used the wrong address or project, exit the host and remove only the entry created for this exercise, then repeat the add command with the intended values:

```bash
# Run from the repository root, Claude Code registration.
claude mcp remove log-triage-telemetry --scope project
```

```bash
# Run from any folder, Codex registration.
codex mcp remove log-triage-telemetry
```

These commands remove a registration, not your project or findings. Preserve unrelated server entries. At the end, stop the HTTP server first so it flushes, then stop the local receiver. Clear the variables in each terminal that used them:

```bash
# Run in each terminal used for the practice.
# macOS and Linux
unset LANGSMITH_API_KEY OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
unset OTEL_EXPORTER_OTLP_HEADERS OTEL_EXPORTER_OTLP_TRACES_HEADERS OTEL_SERVICE_NAME
unset LOG_TRIAGE_TOKENS LOG_TRIAGE_TOKEN LOG_TRIAGE_VIEWER_TOKEN

# Windows (PowerShell)
Remove-Item Env:LANGSMITH_API_KEY, Env:OTEL_EXPORTER_OTLP_ENDPOINT, Env:OTEL_EXPORTER_OTLP_TRACES_ENDPOINT -ErrorAction Ignore
Remove-Item Env:OTEL_EXPORTER_OTLP_HEADERS, Env:OTEL_EXPORTER_OTLP_TRACES_HEADERS, Env:OTEL_SERVICE_NAME -ErrorAction Ignore
Remove-Item Env:LOG_TRIAGE_TOKENS, Env:LOG_TRIAGE_TOKEN, Env:LOG_TRIAGE_VIEWER_TOKEN -ErrorAction Ignore
```

Then delete the practice's key in LangSmith under Settings, API Keys, and remove it from your credential store:

```bash
# Run in any terminal.
# macOS
security delete-generic-password -a "$USER" -s langsmith-api-key

# Linux
secret-tool clear service langsmith-api-key account "$USER"

# Windows (PowerShell)
Remove-Secret -Name langsmith-api-key -Vault SecretStore
```
