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

pulse            # start the daemon, open the printed URL (scan the QR on mobile)
pulse claude     # spawn a session from a terminal and attach; args pass through
```

Needs `tmux`, the agent CLIs you use, and `sqlite3` for OpenCode history.

## Flags

| Flag | Effect |
|------|--------|
| `--local` | Loopback only — no LAN address or public tunnel. |
| `--no-auth` | Drop the token from URLs (trusted networks only). |
| `--quiet` | Suppress notifications. |

`PULSE_NO_TUNNEL=1` skips the public tunnel; `PULSE_DEBUG=1` logs requests.

## How it works

The daemon runs each session in a detached tmux, tails its transcript, and
streams it to the browser over SSE; it never touches your global agent config.
History is read from each tool's own store (`~/.claude`, `~/.codex`, OpenCode's
SQLite). The UI is a Vue app built into a single self-contained `web/index.html`
that the binary embeds.
