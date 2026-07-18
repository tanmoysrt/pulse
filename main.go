package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var validAgents = map[string]bool{"claude": true, "codex": true, "opencode": true}

const defaultPort = 7420

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "ls":
			runList()
			return
		case "attach":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: pulse attach <id>")
				os.Exit(2)
			}
			runAttach(args[1])
			return
		}
	}
	agent, agentArgs, local, noAuth, quiet := parseArgs(args)
	if agent != "" {
		if !validAgents[agent] {
			fmt.Fprintln(os.Stderr, "usage: pulse [--local] [--no-auth] [--quiet] [<claude|codex|opencode> [agent args...]]\n       pulse ls | pulse attach <id>")
			os.Exit(2)
		}
		runClient(agent, agentArgs)
		return
	}
	runDaemon(local, noAuth, quiet)
}

// runList prints the daemon's live sessions.
func runList() {
	live, err := daemonSessions()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse:", err)
		os.Exit(1)
	}
	if len(live) == 0 {
		fmt.Println("no running sessions")
		return
	}
	fmt.Printf("%-4s %-9s %-12s %s\n", "ID", "AGENT", "STATUS", "DIR")
	for _, s := range live {
		fmt.Printf("%-4s %-9s %-12s %s\n", s.ID, s.Tool, s.Status, s.Dir)
	}
	fmt.Println("\nattach with:  pulse attach <id>")
}

// runAttach connects the terminal to a running session's tmux by id.
func runAttach(id string) {
	session := "pulse-" + id
	if !tmuxAlive(session) {
		fmt.Fprintln(os.Stderr, "pulse: no running session with id "+id+" (see: pulse ls)")
		os.Exit(1)
	}
	if err := tmuxAttach(session); err != nil {
		fmt.Fprintln(os.Stderr, "pulse: attach failed:", err)
		os.Exit(1)
	}
}

// daemonSessions fetches the live session list from a running daemon.
func daemonSessions() ([]listItem, error) {
	st, err := readState()
	if err != nil {
		return nil, fmt.Errorf("no daemon running")
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/sessions", st.Port)
	if st.Token != "" {
		url += "?t=" + st.Token
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cannot reach daemon: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Live []listItem `json:"live"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Live, nil
}

// runClient asks the daemon to spawn an agent, then attaches to its tmux.
func runClient(agent string, agentArgs []string) {
	st, err := readState()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: no daemon running. start one first with:  pulse")
		os.Exit(1)
	}
	dir, _ := os.Getwd()
	body, _ := json.Marshal(map[string]any{"agent": agent, "dir": dir, "args": agentArgs})
	url := fmt.Sprintf("http://127.0.0.1:%d/api/sessions", st.Port)
	if st.Token != "" {
		url += "?t=" + st.Token
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: cannot reach daemon:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fmt.Fprintln(os.Stderr, "pulse: spawn failed:", strings.TrimSpace(string(b)))
		os.Exit(1)
	}
	var out struct {
		ID   string `json:"id"`
		Tmux string `json:"tmux"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if err := tmuxAttach(out.Tmux); err != nil {
		fmt.Fprintln(os.Stderr, "pulse: attach failed (reattach with: tmux attach -t "+out.Tmux+"):", err)
		os.Exit(1)
	}
}

func runDaemon(local, noAuth, quiet bool) {
	bindHost := ""
	if local {
		bindHost = "127.0.0.1"
	}
	token := ""
	if !noAuth {
		token = randomToken()
	}

	ln, port := listen(bindHost, defaultPort)
	d := newDaemon(token, quiet, port)
	d.reconcile()
	startServer(d, ln)
	writeState(daemonState{Port: port, Token: token, PID: os.Getpid()})

	fmt.Print(daemonBanner(d, local, port))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	<-ctx.Done()
	stop() // restore default handler so a second Ctrl-C force-quits
	handleShutdown(d)
}

// handleShutdown asks whether to stop running sessions before exiting. Sessions
// live in detached tmux, so declining just leaves them for the next restart.
func handleShutdown(d *Daemon) {
	n := d.count()
	if n == 0 {
		fmt.Println("\npulse: shutting down…")
		d.shutdown()
		return
	}
	if promptStopAll(n) {
		fmt.Println("pulse: stopping all sessions…")
		d.shutdown()
		return
	}
	fmt.Printf("pulse: leaving %d session(s) running (reattach: pulse attach <id>)\n", n)
	d.detach()
}

// promptStopAll asks the user y/N with a 60s timeout; the default (and the
// answer when there's no terminal) is to leave sessions running.
func promptStopAll(n int) bool {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	fmt.Printf("\npulse: %d session(s) still running. Stop all of them? [y/N] (60s) ", n)
	ans := make(chan bool, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			ans <- false
			return
		}
		a := strings.ToLower(strings.TrimSpace(line))
		ans <- a == "y" || a == "yes"
	}()
	select {
	case v := <-ans:
		return v
	case <-time.After(60 * time.Second):
		fmt.Println("\npulse: timed out — leaving sessions running")
		return false
	}
}

// daemonBanner is the startup blurb: UI URLs plus a QR of the best one.
func daemonBanner(d *Daemon, local bool, port int) string {
	localURL := withToken(fmt.Sprintf("http://localhost:%d", port), d.token)

	lanURL := ""
	if !local {
		if ip := lanIP(); ip != "" {
			lanURL = withToken(fmt.Sprintf("http://%s:%d", ip, port), d.token)
		}
	}

	tunnelURL := ""
	if !local && os.Getenv("PULSE_NO_TUNNEL") == "" {
		if u, err := startLocalTunnel(port); err != nil {
			fmt.Fprintln(os.Stderr, "pulse: tunnel unavailable, using local network:", err)
		} else {
			tunnelURL = withToken(u, d.token)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "pulse: daemon ready — open the UI to start a session\n")
	if tunnelURL != "" {
		fmt.Fprintf(&b, "  %s   (public)\n", tunnelURL)
	}
	if lanURL != "" {
		fmt.Fprintf(&b, "  %s   (LAN)\n", lanURL)
	}
	fmt.Fprintf(&b, "  %s\n", localURL)
	if d.token != "" {
		fmt.Fprintf(&b, "  (token auth on; share the URL as-is. bypass with --no-auth)\n")
	}
	fmt.Fprintf(&b, "\n")

	qrURL := tunnelURL
	if qrURL == "" {
		qrURL = lanURL
	}
	if qrURL != "" {
		if qr, err := qrTerminal(qrURL); err == nil {
			fmt.Fprintf(&b, "Scan to open  %s\n\n%s\n", qrURL, qr)
		}
	}
	fmt.Fprintf(&b, "From any terminal: `pulse claude` spawns a session and attaches; `pulse ls` lists them, `pulse attach <id>` reattaches.\nCtrl-C asks whether to stop running sessions (they otherwise keep running for the next start).\n")
	return b.String()
}

// hookSettings builds Claude's per-session settings: HTTP hooks tagged with id.
func hookSettings(port int, id, token string) ([]byte, error) {
	base := fmt.Sprintf("http://localhost:%d/hooks/%s", port, id)
	q := ""
	if token != "" {
		q = "?t=" + token
	}
	hook := func(path string, timeout int) []map[string]any {
		h := map[string]any{"type": "http", "url": base + path + q}
		if timeout > 0 {
			h["timeout"] = timeout
		}
		return []map[string]any{{"hooks": []map[string]any{h}}}
	}
	return json.MarshalIndent(map[string]any{
		"hooks": map[string]any{
			"SessionStart":      hook("/session-start", 0),
			"PermissionRequest": hook("/permission", 900),
			"Stop":              hook("/stop", 0),
		},
	}, "", "  ")
}

// listen prefers a fixed port for a stable URL, falling back to a random one.
func listen(host string, preferred int) (net.Listener, int) {
	if ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, preferred)); err == nil {
		return ln, preferred
	}
	return freePort(host)
}

func freePort(host string) (net.Listener, int) {
	for {
		port := 30001 + rand.Intn(30000)
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			return ln, port
		}
	}
}

// parseArgs strips pulse's own flags; the first remaining positional (if any)
// is the agent to spawn as a client, the rest is forwarded verbatim.
func parseArgs(argv []string) (agent string, agentArgs []string, local, noAuth, quiet bool) {
	var rest []string
	for _, a := range argv {
		switch a {
		case "--local":
			local = true
		case "--no-auth":
			noAuth = true
		case "--quiet":
			quiet = true
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) > 0 {
		agent, agentArgs = rest[0], rest[1:]
	}
	return
}

func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
