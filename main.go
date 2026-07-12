package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var validAgents = map[string]bool{"claude": true, "codex": true, "opencode": true}

func hookSettings(port int, token string) ([]byte, error) {
	base := fmt.Sprintf("http://localhost:%d/hooks", port)
	// The agent's hook callbacks must pass the same token auth as the UI; carry
	// it as a query param, which authMiddleware accepts.
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

func freePort(host string) (net.Listener, int) {
	for {
		port := 30001 + rand.Intn(30000)
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			return ln, port
		}
	}
}

// parseArgs pulls pulse's own flags (--local, --no-auth) out of the argv no
// matter where they appear, so they never reach the agent CLI. The first
// remaining positional is the agent; everything after it is forwarded verbatim.
func parseArgs(argv []string) (agent string, agentArgs []string, local, noAuth bool, ok bool) {
	var rest []string
	for _, a := range argv {
		switch a {
		case "--local":
			local = true
		case "--no-auth":
			noAuth = true
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || !validAgents[rest[0]] {
		return "", nil, local, noAuth, false
	}
	return rest[0], rest[1:], local, noAuth, true
}

func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func main() {
	agent, agentArgs, local, noAuth, ok := parseArgs(os.Args[1:])
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: pulse [--local] [--no-auth] <claude|codex|opencode> [agent args...]")
		os.Exit(2)
	}

	// --local binds the UI to loopback only (no LAN, no public tunnel).
	bindHost := ""
	if local {
		bindHost = "127.0.0.1"
	}

	// Token-based URL auth is on by default; --no-auth is the escape hatch.
	token := ""
	if !noAuth {
		token = randomToken()
	}

	ln, port := freePort(bindHost)

	var extraArgs []string
	cleanup := func() {}
	var ocBase string

	switch agent {
	case "claude":
		settings, err := hookSettings(port, token)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pulse:", err)
			os.Exit(1)
		}
		settingsPath := filepath.Join(os.TempDir(), fmt.Sprintf("pulse-settings-%d.json", port))
		if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "pulse:", err)
			os.Exit(1)
		}
		extraArgs = []string{"--settings", settingsPath}
		cleanup = func() { os.Remove(settingsPath) }
	case "codex":
		extraArgs = codexHookArgs(token)
	case "opencode":
		ocLn, ocPort := freePort("127.0.0.1")
		ocLn.Close()
		ocBase = fmt.Sprintf("http://127.0.0.1:%d", ocPort)
		extraArgs = []string{"--port", strconv.Itoa(ocPort), "--hostname", "127.0.0.1"}
	}
	defer cleanup()

	// Start the web server up front so the tunnel and any early visitor get a
	// live loading screen while we wait at the "Press Enter" prompt below. The
	// agent (tmux session) isn't launched until Enter, so the UI just shows its
	// idle/boot state until then.
	session := fmt.Sprintf("pulse-%d", port)
	srv := newServer(session, agent)
	srv.ocBase = ocBase
	srv.token = token
	startServer(srv, ln)
	defer os.RemoveAll(srv.uploadDir())

	localURL := withToken(fmt.Sprintf("http://localhost:%d", port), token)

	// --local keeps everything on loopback: no LAN address, no public tunnel.
	lanURL := ""
	if !local {
		if ip := lanIP(); ip != "" {
			lanURL = withToken(fmt.Sprintf("http://%s:%d", ip, port), token)
		}
	}

	// Expose the session publicly by default via localtunnel; on any failure
	// (offline, service down) fall back to the LAN URL. Opt out: PULSE_NO_TUNNEL
	// or --local.
	var tunnelURL string
	if !local && os.Getenv("PULSE_NO_TUNNEL") == "" {
		if u, err := startLocalTunnel(port); err != nil {
			fmt.Fprintln(os.Stderr, "pulse: tunnel unavailable, using local network:", err)
		} else {
			tunnelURL = withToken(u, token)
		}
	}

	var banner strings.Builder
	fmt.Fprintf(&banner, "pulse: web UI ready\n")
	if tunnelURL != "" {
		fmt.Fprintf(&banner, "  %s   (public)\n", tunnelURL)
	}
	if lanURL != "" {
		fmt.Fprintf(&banner, "  %s   (LAN)\n", lanURL)
	}
	fmt.Fprintf(&banner, "  %s\n", localURL)
	if token != "" {
		fmt.Fprintf(&banner, "  (token auth on; share the URL as-is. bypass with --no-auth)\n")
	}
	fmt.Fprintf(&banner, "\n")

	// QR encodes the best reachable address: the public tunnel if we have one,
	// otherwise the LAN address (localhost is useless from a phone).
	qrURL := tunnelURL
	if qrURL == "" {
		qrURL = lanURL
	}
	if qrURL != "" {
		if qr, err := qrTerminal(qrURL); err == nil {
			fmt.Fprintf(&banner, "Scan to open  %s\n\n%s\n", qrURL, qr)
		}
	}
	fmt.Fprintf(&banner, "Press %s anytime inside the session to show this again.\n", linksHotkey)

	fmt.Print(banner.String())

	// Stash the banner so the F12 popup (bound once the agent's tmux session
	// exists, in tmuxHideChrome) can redisplay it later without leaving the
	// agent CLI or rerunning pulse.
	if err := tmuxWriteLinks(session, banner.String()); err == nil {
		defer os.Remove(linksPath(session))
	}

	fmt.Printf("\nPress Enter to start %s... ", agent)
	bufio.NewReader(os.Stdin).ReadString('\n')

	if agent == "opencode" {
		cwd, _ := os.Getwd()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go opencodePoll(ctx, srv, ocBase, cwd, time.Now())
	}

	srv.markStarted()

	args := append(append([]string{}, agentArgs...), extraArgs...)
	env := []string{fmt.Sprintf("PULSE_PORT=%d", port)}
	err := tmuxRun(session, env, append([]string{agent}, args...))
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: tmux:", err)
		os.Exit(1)
	}
}
