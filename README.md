# Pulse

**Drive your terminal coding agents from your phone.**

Pulse is a small daemon that gives you one web UI for every
[Claude Code](https://claude.com/claude-code), [Codex](https://openai.com/codex),
and [OpenCode](https://opencode.ai) session on your machine — browse past
transcripts, start or resume a chat, and watch it live: send prompts, approve
tools, switch models, get pinged when it needs you.

```bash
(cd frontend && npm install && npm run build)   # build the UI
go build -o pulse .

pulse            # guided setup, then prints the URL + QR (scan it on mobile)
pulse claude     # spawn a session from a terminal and attach; args pass through
```

Needs `tmux`, the agent CLIs you use, and `sqlite3` for OpenCode history.

On an interactive terminal `pulse` walks you through how it should be reachable
(LAN or a public tunnel), a login password (random if you leave it blank), and
whether to pop desktop notifications. Scanning the QR logs you straight in;
opening the link by hand asks for the password (rate-limited to 5 tries per
15 min). Pass any of the flags below to skip the matching prompt.

## Flags

| Flag | Effect |
|------|--------|
| `--lan` / `--tunnel` | Choose LAN or a public tunnel without being asked. |
| `--local` | Loopback only — no LAN address or public tunnel. |
| `--password <pw>` | Set the login password (else a random one is generated). |
| `--notify` | Enable desktop notifications on this machine. |
| `--no-auth` | Drop auth entirely (trusted networks only). |

`PULSE_DEBUG=1` logs requests.

## How it works

The daemon runs each session in a detached tmux, tails its transcript, and
streams it to the browser over SSE; it never touches your global agent config.
History is read from each tool's own store (`~/.claude`, `~/.codex`, OpenCode's
SQLite). The UI is a Vue app built into a single self-contained
`frontend/dist/index.html` that the binary embeds.
