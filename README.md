# Pulse

**Drive your terminal coding agent from your phone.**

Pulse wraps a single [Claude Code](https://claude.com/claude-code), [Codex](https://openai.com/codex), or [OpenCode](https://opencode.ai) session in a live web UI. Watch the transcript stream in real time, send prompts, approve tool permissions, switch models, and get a push notification the moment the agent needs you or finishes.

Kick off a long run, walk away, and the session comes with you. Approve that `rm` from the couch, redirect the agent from the kitchen, get pinged when it's done.

```
pulse claude
```

Scan the QR it prints, and your session is on your phone.

## Features

- **Live transcript**: messages, thinking, tool calls, and results stream over SSE.
- **Remote control**: send prompts, interrupt, start fresh, switch model / effort / mode, attach files.
- **Permission approvals**: approve or deny tool runs from the UI (or fall back to the terminal).
- **Push notifications**: alerts when the agent finishes or needs you, even with the tab closed.
- **Reach it anywhere**: localhost, LAN IP, and a public HTTPS tunnel, all shown as URLs + a QR.
- **Token auth by default**: every URL carries a random secret.
- **Three agents, one UI**: Claude Code, Codex, and OpenCode.

## Usage

Needs `tmux` and one of the agent CLIs on your `PATH`.

```bash
go build -o pulse .

pulse claude                 # or: pulse codex | pulse opencode
pulse claude --model opus    # extra args pass through to the agent
```

Pulse prints the URLs and a QR code, then waits. **Press Enter** to start the agent in a tmux session your terminal attaches to. Press **F12** inside the session anytime to show the URLs again.

### Flags

| Flag | Effect |
|------|--------|
| `--local` | Loopback only, no LAN address or public tunnel. |
| `--no-auth` | Drop the token from URLs (trusted networks only). |
| `--quiet` | Suppress notifications. |

`PULSE_NO_TUNNEL=1` skips the public tunnel; `PULSE_DEBUG=1` logs requests.

## How it works

Pulse never touches your global agent config. It generates per-session hooks that call back over HTTP, tails the transcript the agent writes and streams it to the browser over SSE, and relays your input by typing into the tmux pane. When tmux exits, so does pulse.
