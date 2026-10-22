# Agent hosts

**Related**:

> [README](README.md): Tasks 4 to 7 follow these steps
> [CONTRACT](CONTRACT.md): Section 5 for the prompt, section 7 for callers and HTTP
> [cases](cases.md): The questions to ask through a host

How to connect two agent hosts to your server over HTTP: Claude Code first, and Codex CLI as the second host. Both talk to the same running server, which tells callers apart only by their bearer tokens.

## Links
- [Claude Code: connect to tools via MCP](https://code.claude.com/docs/en/mcp)
- [Codex: Model Context Protocol](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
- [MCP Inspector CLI](https://modelcontextprotocol.io/docs/2026-07-28/tools/inspector/cli)

**Internal**:

- `4-agents-and-clients/tokens.example.env`
- `4-agents-and-clients/CONTRACT.md`

## Before either host
1. From the repository root, load the example tokens into your shell:

   ```bash
   # Run from the repository root in the first terminal.
   set -a; . 4-agents-and-clients/tokens.example.env; set +a
   ```

2. In that shell, start your server with `--http 127.0.0.1:8765`, as the practice README shows, and leave it running. It serves `http://127.0.0.1:8765/mcp`.
3. Open a second terminal at the repository root and load the tokens there too. The hosts read `LOG_TRIAGE_TOKEN` from that shell.

Each host below sends `Authorization: Bearer <token>` with every request. Without it, the server answers 401.

## Claude Code
### Register
From the repository root, in the second terminal:

```bash
# Run from the repository root in the second terminal.
claude mcp add --scope project --transport http log-triage http://127.0.0.1:8765/mcp \
  --header 'Authorization: Bearer ${LOG_TRIAGE_TOKEN}'
claude mcp get log-triage
```

- `--scope project` writes the entry to `.mcp.json` at the repository root, which `.gitignore` keeps out of git. Without the flag, Claude Code writes it to your own settings in your home folder.
- The single quotes keep `${LOG_TRIAGE_TOKEN}` as written. Claude Code puts in the variable's value each time it connects, so the token itself is never stored in the file.
- Until you approve the server, `claude mcp get` shows it as pending. Claude Code asks for the approval when you start `claude` in this folder.
- If you registered the server over stdio in an earlier practice, remove that entry first so there is one `log-triage`.

If Claude Code has the wrong command or URL, run only the command for the scope that contains it, then repeat the registration above:

```bash
# Run from the repository root in the second terminal.
claude mcp remove log-triage --scope local
claude mcp remove log-triage --scope project
```

### Use
Start a session in which the agent has no built-in tools, only your server's:

```bash
# Run from the repository root in the second terminal.
claude --tools ""
```

`--tools ""` removes Claude Code's built-in tools, not other MCP servers you have added. If `/mcp` lists any besides `log-triage`, turn them off there for this project, and back on the same way when you're done.

- `/mcp` shows the server, whether it is connected, and its tools.
- The prompt runs as a command: `/mcp__log-triage__triage <symptom> <path> <since> <until>`. Claude Code splits what follows on whitespace, one word per argument, keeps quotes as characters and drops words past the fourth. So write the symptom as one hyphenated word, leave out trailing arguments you don't need, and ask your question in full in the next message. For example, with made-up values: `/mcp__log-triage__triage slow-checkout vms 2026-01-10 2026-01-11`.
- Claude Code asks for your permission before it runs a tool you haven't allowed yet. Allowing the read tools for the session is fine; read what `save_finding` is about to write before you allow it.
- To see the server as the read-only caller does, start the session with the other token: `LOG_TRIAGE_TOKEN="$LOG_TRIAGE_VIEWER_TOKEN" claude --tools ""`. `save_finding` is missing from its tools.

## Codex CLI
### Register
In the second terminal:

```bash
# Run from the repository root in the second terminal.
codex mcp add log-triage --url http://127.0.0.1:8765/mcp --bearer-token-env-var LOG_TRIAGE_TOKEN
codex mcp get log-triage
```

- This writes the entry to Codex's own settings in your home folder, `~/.codex/config.toml`, with the variable's name, not the token. Codex also reads a `.codex/config.toml` inside a project you have marked as trusted.
- To have Codex ask before it runs a tool that isn't marked read-only, which on this server is only `save_finding`, add this line to the `[mcp_servers.log-triage]` table of that file:

  ```toml
  default_tools_approval_mode = "writes"
  ```

- When the practice is done, `codex mcp remove log-triage` takes the entry out.

If Codex has the wrong command or URL, remove the entry and repeat the registration above:

```bash
# Run from the repository root in the second terminal.
codex mcp remove log-triage
```

### Use
Start `codex` in the second terminal, and paste the case question as your message. `/mcp` in Codex lists the servers it has connected and their tools.

## Check without a host
`inspector.json` in this folder holds three HTTP entries for the MCP Inspector: one that sends the first example token, one that sends the read-only token, and one that sends none. The Inspector's configuration file takes header values as written, so it carries the example tokens themselves rather than a variable. The README's verify step runs all three.
