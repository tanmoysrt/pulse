# Pulse — remote UI for a single Claude Code session

One Go binary (Echo + embedded Vue PWA). You launch your session as:

```
pulse claude [any claude args...]
```

Pulse picks a random free port > 30000, prints the URL to open (LAN IP + localhost), and waits — **press Enter** and it starts the claude TUI in a tmux session your terminal attaches to. You interact with claude directly as normal; pulse just taps in for the web: a UI **for this chat only** — live transcript over SSE, composer to send messages, approve/deny permission prompts. No flags, no daemon, no session list, no history browser, no tests. Plain HTTP, no auth. Plan committed as `plan.md`.

## How it hooks in (verified against Claude Code docs)

- After Enter, pulse runs `tmux new-session -s pulse-<port> claude --settings <generated JSON> <args...>` (attached, foreground; pulse exits when tmux does). The `--settings` flag loads extra settings for **this invocation only**, so hooks are session-scoped; global `~/.claude/settings.json` is never touched. Generated hooks (all `type:"http"` pointing at `localhost:<port>`):
  - `SessionStart` → tells pulse the `session_id` + `transcript_path`
  - `PermissionRequest` → POSTs `{tool_name, tool_input, ...}` and waits (timeout 900) for `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"|"deny"}}}`; while pending, the web UI shows Approve/Deny. On timeout/pulse issues claude falls back to its terminal prompt.
  - `Stop` → marks the turn finished (status pill), returns 200.
- Transcript: pulse tails `transcript_path` (the session's `.jsonl`, written continuously by claude) and pushes parsed messages to the browser over **SSE**.
- Send: composer POSTs text; pulse runs `tmux send-keys -t pulse-<port> -l <text>` then `Enter`. No optimistic rendering — the sent message lands in the `.jsonl` and flows back through SSE like everything else (slight latency is fine).

```
pulse claude ...
 ├─ random port >30000, print URL, wait for Enter
 ├─ tmux new-session (attached) ── you ⇄ claude (spawned with --settings hooks JSON)
 ├─ tails <session>.jsonl (path from SessionStart hook)
 └─ Echo on 0.0.0.0:<port>
      /hooks/*  ← claude          /  ← embedded Vue app
      /api/*    ← browser          /api/events → SSE (messages + state)
                                   send → tmux send-keys
```

## Layout

```
main.go        # argv passthrough, random port, print URL + wait Enter, start tmux + server, exit with tmux
tmux.go        # new-session / send-keys helpers (exec.Command)
server.go      # Echo: hook endpoints + API + SSE broker + embedded dist
transcript.go  # tail jsonl (fs poll ~500ms from byte offset), normalize to {line, role, kind, text}
embed.go       # //go:embed web/dist
web/           # Vue 3 + Vite; dist/ committed so `go build` needs no npm
plan.md
```

State is a single mutex-guarded struct: `sessionID`, `transcriptPath`, `status` (`running`/`idle`/`needs_approval`), `pending *PermissionReq` (decision chan), plus subscriber channels for SSE fan-out.

**API**:
- `GET /api/events` — SSE. On connect, replays transcript from line 0 (or `Last-Event-ID`), then streams live: `message` events (parsed transcript lines, id = line number) and `state` events (`{status, pending: {id, toolName, toolInput} | null}`).
- `POST /api/send {text}` → tmux send-keys
- `POST /api/permission {id, decision}` → resolves the held-open hook

**Web UI** (single view, no router): chat transcript fed by one `EventSource` (auto-reconnect + Last-Event-ID resume for free), auto-scroll, composer at bottom, permission banner with Approve/Deny, status pill. Mobile-first CSS. PWA manifest + minimal sw.js registered only on secure contexts (SW needs HTTPS/localhost; over LAN HTTP it's a normal page).

## Build order

1. `main.go` + `tmux.go` + `--settings` generation — `pulse claude` prints URL, Enter launches tmux with hooks wired
2. `server.go` hooks — verify: permission held open, approve via curl unblocks it; SessionStart/Stop captured
3. `transcript.go` + SSE — verify streamed messages match the `.jsonl`
4. Vue app → build → embed, commit `dist/`
5. Commit + PR

## Verification (in this sandbox, normal TUI only — no `claude -p`)

1. `go build -o pulse .`; run `./pulse claude` in a scratch dir (Enter piped in; tmux stays detached here since the sandbox has no interactive terminal — locally it attaches). Confirm printed URL, TUI up via `tmux capture-pane`, SessionStart captured.
2. `POST /api/send {"text":"list files here"}` → prompt appears in the pane, claude runs; the Bash permission request arrives as an SSE `state` event, `POST /api/permission {"decision":"allow"}` unblocks it, Stop flips status to `idle`, streamed messages match the transcript `.jsonl`.
3. Playwright headless Chromium on the printed URL: transcript renders live via SSE, composer send round-trips (sent text shows up once it hits the jsonl), Approve in the banner unblocks a live permission end-to-end.
4. Confirm `~/.claude/settings.json` is never modified.
