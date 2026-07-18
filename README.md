# Pulse

**Drive your terminal coding agents from your phone.**

Pulse runs as a small foreground daemon that gives you one web UI for every
[Claude Code](https://claude.com/claude-code), [Codex](https://openai.com/codex),
and [OpenCode](https://opencode.ai) session on your machine. It lists your past
transcripts across all three tools, lets you start a new chat from the UI (or
the terminal), and streams each live session — watch the transcript, send
prompts, approve tool permissions, switch models, and get a push notification
the moment an agent needs you or finishes.

Kick off a long run, walk away, and the session comes with you. Approve that
`rm` from the couch, redirect the agent from the kitchen, get pinged when it's
done.

```
pulse
```

Scan the QR it prints, and every session is on your phone.

## Features

- **One daemon, every session**: a single UI for Claude Code, Codex, and OpenCode.
- **Cross-tool history**: browse and read past transcripts from all three tools.
- **Resume**: reopen any past transcript as a live session (calls each CLI's own resume).
- **Start from anywhere**: new chats from the UI (pick a directory) or `pulse claude` in a terminal.
- **Live transcript**: messages, thinking, tool calls, and results stream over SSE.
- **Remote control**: send prompts, interrupt, start fresh, switch model / effort / mode, attach files.
- **Permission approvals**: approve or deny tool runs from the UI (or fall back to the terminal).
- **Push notifications**: alerts when an agent finishes or needs you, even with the tab closed.
- **Reach it anywhere**: localhost, LAN IP, and a public HTTPS tunnel, all shown as URLs + a QR.
- **Token auth by default**: every URL carries a random secret.

## Usage

Needs `tmux`, `sqlite3` (for OpenCode history), and the agent CLIs you want on your `PATH`.

```bash
# Build the UI (once, or after changing frontend/), then the binary:
(cd frontend && npm install && npm run build)
go build -o pulse .

pulse                        # start the daemon; open the printed URL
pulse claude                 # from any terminal: spawn a session and attach to it
pulse codex --model gpt-5.5  # extra args pass through to the agent
```

`pulse` prints the URLs and a QR, then keeps running. Open the UI to see your
history and start sessions. `pulse <agent>` asks the running daemon to spawn a
session in the current directory and attaches your terminal to it; detach
(`Ctrl-b d`) and it keeps running — reopen it from the UI or reattach with
`tmux attach`.

### Flags (daemon)

| Flag | Effect |
|------|--------|
| `--local` | Loopback only, no LAN address or public tunnel. |
| `--no-auth` | Drop the token from URLs (trusted networks only). |
| `--quiet` | Suppress notifications. |

`PULSE_NO_TUNNEL=1` skips the public tunnel; `PULSE_DEBUG=1` logs requests.

## How it works

Pulse never touches your global agent config. The daemon binds a stable port
(default 7420) and writes it, with the auth token, to a small state file so
`pulse <agent>` clients can find it. For each session it generates per-session
hooks that call back over HTTP (`/hooks/<id>/…`), launches the agent in a
detached tmux session, tails the transcript the agent writes, and streams it to
the browser over SSE. History is read straight from each tool's own store —
`~/.claude/projects` and `~/.codex/sessions` as JSONL, OpenCode from its SQLite
database. Resuming a past transcript runs that CLI's own resume
(`claude --resume`, `codex resume`, `opencode --session`) in a fresh session
and adopts its transcript directly, since some CLIs don't fire hooks on resume.
Closing a session kills its tmux; Ctrl-C stops the daemon.

## Frontend

The UI is a Vue 3 + Vite app in `frontend/`, built into a single self-contained
`web/index.html` (via `vite-plugin-singlefile`) that the Go binary embeds — no
runtime CDN dependency. During UI development, `cd frontend && npm run dev`
proxies nicely if you point it at a running daemon; for release just
`npm run build` and rebuild the Go binary.
