package main

import (
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
)

var validAgents = map[string]bool{"claude": true, "codex": true, "opencode": true}

const defaultPort = 7420

func main() {
	agent, agentArgs, local, noAuth, quiet := parseArgs(os.Args[1:])
	if agent != "" {
		if !validAgents[agent] {
			fmt.Fprintln(os.Stderr, "usage: pulse [--local] [--no-auth] [--quiet] [<claude|codex|opencode> [agent args...]]")
			os.Exit(2)
		}
		runClient(agent, agentArgs)
		return
	}
	runDaemon(local, noAuth, quiet)
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
	startServer(d, ln)
	writeState(daemonState{Port: port, Token: token, PID: os.Getpid()})

	fmt.Print(daemonBanner(d, local, port))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	fmt.Println("\npulse: shutting down…")
	d.shutdown()
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
	fmt.Fprintf(&b, "Also runnable from any terminal: `pulse claude` spawns a session here and attaches.\nPress Ctrl-C to stop the daemon.\n")
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
