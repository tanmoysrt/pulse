# Pulse

Drive your terminal coding agents from your phone.

Pulse is a small daemon that puts every
[Claude Code](https://claude.com/claude-code), [Codex](https://openai.com/codex),
and [OpenCode](https://opencode.ai) session on your machine behind one web UI.
Browse past transcripts, start or resume a chat, and follow it live: send
prompts, approve tools, switch models, and get notified when a session needs you.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/tanmoysrt/pulse/master/install.sh | sh
```

This downloads the right binary for your machine (macOS or Linux, amd64 or
arm64) into `/usr/bin/pulse`. Run it again any time to update; it asks first.

Pulse needs `tmux`, the agent CLIs you use, and `sqlite3` for OpenCode history.

## Usage

```bash
pulse             # start the daemon, then print the URL and a QR code to scan
pulse claude      # spawn a session from the terminal and attach; args pass through
pulse ls          # list running sessions
pulse attach <id> # reattach to a running session
```

The first run walks you through how Pulse should be reachable (LAN or a public
tunnel), a login password (random if you leave it blank), and whether to show
desktop notifications. Scanning the QR code logs you straight in. Opening the
link by hand asks for the password, rate limited to 5 tries per 15 minutes.
Pass any flag below to skip its prompt.

## Flags

| Flag | Effect |
|------|--------|
| `--lan` / `--tunnel` | Choose LAN or a public tunnel without being asked. |
| `--local` | Loopback only, with no LAN address or public tunnel. |
| `--listen-port <n>` | Port to serve on (default 4444). |
| `--password <pw>` | Set the login password (otherwise one is generated). |
| `--notify` | Enable desktop notifications on this machine. |
| `--no-auth` | Drop auth entirely (trusted networks only). |

Set `PULSE_DEBUG=1` to log requests.

## How it works

Each session runs in a detached tmux, and Pulse streams its transcript to the
browser. It reads history from each tool's own store and never touches your
global agent config.

## Build from source

```bash
make prod        # builds the UI and stripped binaries for every platform in dist/
```

## License

[Apache 2.0](LICENSE)
