package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
)

var validAgents = map[string]bool{"claude": true, "codex": true, "opencode": true}

const defaultPort = 4444

const installScriptURL = "https://raw.githubusercontent.com/tanmoysrt/pulse/master/install.sh"

// daemonResumeEnv and friends are internal parent->child signaling for
// runDetach's re-exec, not documented user flags — same convention as the
// PULSE_PORT env var passed into agent tmux sessions.
const (
	daemonResumeEnv         = "PULSE_DAEMON_RESUME"
	daemonResumeTokenEnv    = "PULSE_RESUME_TOKEN"
	daemonResumePassHashEnv = "PULSE_RESUME_PWHASH"
)

// version is stamped at build time via -ldflags "-X main.version=..."; "dev"
// for a plain `go build`.
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println(version)
			return
		case "ls":
			runList()
			return
		case "update":
			runUpdate()
			return
		case "attach":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: pulse attach <id>")
				os.Exit(2)
			}
			runAttach(args[1])
			return
		case "stop":
			runStop()
			return
		case "add-domain":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: pulse add-domain <domain-or-ip>")
				os.Exit(2)
			}
			runAddDomain(args[1])
			return
		}
	}
	agent, agentArgs, o := parseArgs(args)
	if agent != "" {
		if !validAgents[agent] {
			fmt.Fprintln(os.Stderr, "usage: pulse [--lan|--tunnel|--local|--acme <domain>] [--password <pw>] [--listen-port <n>] [--notify] [<claude|codex|opencode> [agent args...]]\n       pulse ls | pulse attach <id> | pulse stop | pulse update")
			os.Exit(2)
		}
		runClient(agent, agentArgs)
		return
	}
	if err := checkRequirements(); err != nil {
		fmt.Fprintln(os.Stderr, "pulse:", err)
		os.Exit(1)
	}
	if st, err := readState(); err == nil && processAlive(st.PID) {
		printStatus(st)
		return
	}
	runDaemon(o)
}

func checkRequirements() error {
	for _, command := range []string{"tmux", "sqlite3"} {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("%s is required; install it and run pulse again", command)
		}
	}
	return nil
}

// runUpdate streams the official installer to sh. The installer reads its
// confirmation from /dev/tty, so users can still answer its prompts.
func runUpdate() {
	fetch := exec.Command("curl", "-fsSL", installScriptURL)
	script, err := fetch.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not fetch update script:", err)
		os.Exit(1)
	}
	fetch.Stderr = os.Stderr

	install := exec.Command("sh")
	install.Stdin = script
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr

	if err := fetch.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not start update:", err)
		os.Exit(1)
	}
	installErr := install.Run()
	fetchErr := fetch.Wait()
	if fetchErr != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not fetch update script:", fetchErr)
		os.Exit(1)
	}
	if installErr != nil {
		fmt.Fprintln(os.Stderr, "pulse: update failed:", installErr)
		os.Exit(1)
	}
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

func runDaemon(o opts) {
	// Captured once, before this process could ever rename its own binary
	// aside (see replaceBinary) — os.Executable() re-derived after that point
	// would resolve to the renamed old file instead of the new one.
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not locate own executable:", err)
		return
	}

	resumeToken := os.Getenv(daemonResumeTokenEnv)
	resuming := os.Getenv(daemonResumeEnv) == "1" && resumeToken != ""

	if !resuming {
		o = runWizard(o) // interactive prompts for anything not fixed by a flag
	}

	bindHost := ""
	if o.local {
		bindHost = "127.0.0.1"
	}

	var token, passwordHash string
	if resuming {
		// Re-exec'd by runDetach: reuse the live token/hash so already-issued
		// session cookies and the hook-auth token stay valid across the move
		// to the background.
		token, passwordHash = resumeToken, os.Getenv(daemonResumePassHashEnv)
	} else {
		token = randomToken()
		passwordHash = o.passwordHash
		if passwordHash == "" {
			password := strings.TrimSpace(o.password)
			if password == "" {
				fmt.Fprintln(os.Stderr, "pulse: a login password is required")
				return
			}
			var err error
			passwordHash, err = hashPassword(password)
			if err != nil {
				fmt.Fprintln(os.Stderr, "pulse: could not secure password:", err)
				return
			}
		}
	}

	pref := defaultPort
	if o.port > 0 {
		pref = o.port
	} else if o.acme {
		pref = 443
	}

	var tlsCert *tls.Certificate
	if o.acme {
		cert, err := loadCert(o.domain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pulse: no certificate for "+o.domain+" yet — run: pulse add-domain "+o.domain)
			return
		}
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil && time.Now().After(leaf.NotAfter) {
			fmt.Printf("pulse: certificate for %s expired on %s — serving it anyway; renew with: pulse add-domain %s\n", o.domain, leaf.NotAfter.Format("2006-01-02"), o.domain)
		}
		tlsCert = cert
	}

	ln, port := listen(bindHost, pref)
	if tlsCert != nil {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{*tlsCert}})
	}
	d := newDaemon(token, passwordHash, o.localNotify, port)
	defer d.stopSleepInhibitor()
	writeSetup(setupRecord{Tunnel: o.tunnel, Acme: o.acme, Domain: o.domain, Notify: o.localNotify, PasswordHash: passwordHash})
	d.reconcile()
	d.startSleepInhibitor()
	go d.stats.collect()

	urls, primary := resolveURLs(o, port)
	d.urls, d.primary = urls, primary // exposed via /api/status so a later `pulse` can reprint this exact banner
	d.tunnel = o.tunnel
	d.exePath = exePath

	e := startServer(d, ln)
	writeState(daemonState{Port: port, Token: token, PID: os.Getpid()})

	shown := daemonBanner(d, urls, primary)
	fmt.Print(shown)
	fmt.Println(dimStyle.Render(`type "bg" + Enter to move to the background`))
	go watchBootstrapRotation(d, urls, primary, shown)

	go watchDetach(d)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
		stop() // restore default handler so a second Ctrl-C force-quits
		handleShutdown(d)
	case <-d.restart:
		stop()
		runDetach(d, o, port, token, passwordHash, exePath, e)
	}
}

// watchDetach reads lines from stdin and requests a restart when it sees
// "bg" — the same trigger a completed self-update uses. Only meaningful with
// a real terminal attached; a closed/non-tty stdin just makes the read loop
// exit without ever firing.
func watchDetach(d *Daemon) {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return
	}
	r := bufio.NewReader(os.Stdin)
	for {
		line, err := r.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) == "bg" {
			d.requestRestart()
			return
		}
		if err != nil {
			return
		}
	}
}

// runDetach persists session state, stops the HTTP server, and re-execs the
// daemon as a fully detached process (new session, output to a log file) so
// it survives the terminal closing. The port is freed before the child binds
// so it deterministically reclaims the same one — no fd-passing needed.
// exePath is runDaemon's startup-captured path, not a fresh os.Executable()
// call — see runDaemon's comment: after a self-update renames the running
// binary aside, a fresh lookup would resolve to that renamed old file.
func runDetach(d *Daemon, o opts, port int, token, passwordHash, exePath string, e *echo.Echo) {
	fmt.Println("\npulse: moving to background…")
	d.persist()
	d.stopSleepInhibitor()
	removeState()
	e.Shutdown(context.Background())

	logPath := filepath.Join(filepath.Dir(statePath()), "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not open log file:", err)
		os.Exit(1)
	}
	defer logFile.Close()

	cmd := exec.Command(exePath, detachArgs(o, port)...)
	cmd.Env = append(os.Environ(),
		daemonResumeEnv+"=1",
		daemonResumeTokenEnv+"="+token,
		daemonResumePassHashEnv+"="+passwordHash)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not move to background:", err)
		os.Exit(1)
	}
	fmt.Printf("pulse: running in background (pid %d)\n  logs:   %s\n  status: pulse\n  stop:   pulse stop\n", cmd.Process.Pid, logPath)
}

// detachArgs rebuilds the resolved flags for runDetach's re-exec so the
// backgrounded daemon boots with identical config and skips the wizard.
func detachArgs(o opts, port int) []string {
	args := []string{"--listen-port", strconv.Itoa(port)}
	switch {
	case o.local:
		args = append(args, "--local")
	case o.acme:
		args = append(args, "--acme", o.domain)
	case o.tunnel:
		args = append(args, "--tunnel")
	default:
		args = append(args, "--lan")
	}
	if o.localNotify {
		args = append(args, "--notify")
	}
	return args
}

// processAlive reports whether pid names a live process (Unix liveness
// probe: os.FindProcess always succeeds, so the signal is the real check).
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// printStatus reprints the running daemon's own startup banner — same URLs,
// same still-valid QR — by asking it directly instead of starting a second
// daemon. Pulled live from /api/status rather than reconstructed locally
// since the tunnel URL and bootstrap token are only ever known in-process,
// never persisted to daemon.json.
func printStatus(st *daemonState) {
	urls, primary, bootstrap, err := fetchStatus(st)
	if err != nil {
		fmt.Printf("pulse: already running (pid %d) but unreachable: %v\n", st.PID, err)
		return
	}
	footer := fmt.Sprintf("pid %d · stop: pulse stop", st.PID)
	fmt.Print(renderSummary(urls, qrOf(withToken(primary, bootstrap)), footer))
}

// fetchStatus asks a running daemon for the banner data it resolved at
// startup (urls/primary/bootstrap — see Daemon.apiStatus).
func fetchStatus(st *daemonState) (urls []string, primary, bootstrap string, err error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/status", st.Port)
	if st.Token != "" {
		url += "?t=" + st.Token
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	var out struct {
		URLs      []string `json:"urls"`
		Primary   string   `json:"primary"`
		Bootstrap string   `json:"bootstrap"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, "", "", err
	}
	return out.URLs, out.Primary, out.Bootstrap, nil
}

// runStop signals a running daemon to shut down and waits for it to exit.
func runStop() {
	st, err := readState()
	if err != nil || !processAlive(st.PID) {
		fmt.Fprintln(os.Stderr, "pulse: no daemon running")
		os.Exit(1)
	}
	proc, _ := os.FindProcess(st.PID)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not stop daemon:", err)
		os.Exit(1)
	}
	fmt.Print("pulse: stopping…")
	deadline := time.Now().Add(10 * time.Second)
	for processAlive(st.PID) && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if processAlive(st.PID) {
		fmt.Printf("\npulse: still running after 10s (pid %d)\n", st.PID)
		os.Exit(1)
	}
	fmt.Println(" stopped")
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
		fmt.Println("\npulse: timed out, leaving sessions running")
		return false
	}
}

// resolveURLs figures out the primary URL and the full list to display. Call
// once per startup — it may start a tunnel process.
func resolveURLs(o opts, port int) (urls []string, primary string) {
	if o.acme {
		url := fmt.Sprintf("https://%s", o.domain)
		if port != 443 {
			url = fmt.Sprintf("https://%s:%d", o.domain, port)
		}
		return []string{url}, url
	}
	localhost := fmt.Sprintf("http://localhost:%d", port)
	urls = []string{localhost}
	primary = localhost
	if o.tunnel && !o.local {
		if u, err := startLocalTunnel(port); err != nil {
			fmt.Fprintln(os.Stderr, "pulse: tunnel unavailable, falling back to LAN:", err)
		} else {
			primary = u
			urls = append([]string{u}, urls...)
		}
	}
	if !o.local {
		ips := interfaceIPs()
		reorderPreferredFirst(ips)
		for _, ip := range ips {
			url := fmt.Sprintf("http://%s:%d", ip, port)
			urls = append(urls, url)
			if primary == localhost {
				primary = url
			}
		}
	}
	return urls, primary
}

// daemonBanner is the startup screen: the URL list and its QR.
func daemonBanner(d *Daemon, urls []string, primary string) string {
	return renderSummary(urls, qrOf(withToken(primary, d.currentBootstrap())), "Ctrl-C quits")
}

// watchBootstrapRotation redraws the QR whenever its token rotates.
func watchBootstrapRotation(d *Daemon, urls []string, primary, shown string) {
	for range d.bootstrapRotated {
		if lines := strings.Count(shown, "\n"); lines > 0 {
			fmt.Printf("\x1b[%dA\x1b[0J", lines)
		}
		shown = daemonBanner(d, urls, primary)
		fmt.Print(shown)
	}
}

func qrOf(url string) string {
	qr, err := qrTerminal(url)
	if err != nil {
		return ""
	}
	return qr
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

// opts holds daemon startup choices. The *Set fields record whether a flag fixed
// the value, so the wizard only prompts for what the user left open.
type opts struct {
	local, tunnel, acme, localNotify                    bool
	password, domain                                    string
	port                                                int
	tunnelSet, acmeSet, notifySet, passwordSet, portSet bool
	passwordHash                                        string
}

// parseArgs strips pulse's own flags; the first remaining positional (if any)
// is the agent to spawn as a client, the rest is forwarded verbatim.
func parseArgs(argv []string) (agent string, agentArgs []string, o opts) {
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch a {
		case "--local":
			o.local = true
		case "--tunnel":
			o.tunnel, o.tunnelSet = true, true
		case "--lan":
			o.tunnel, o.tunnelSet = false, true
		case "--acme":
			if i+1 < len(argv) {
				i++
				o.domain, o.acme, o.acmeSet = argv[i], true, true
			}
		case "--notify":
			o.localNotify, o.notifySet = true, true
		case "--password":
			if i+1 < len(argv) {
				i++
				o.password, o.passwordSet = argv[i], true
			}
		case "--listen-port":
			if i+1 < len(argv) {
				i++
				o.port, _ = strconv.Atoi(argv[i])
				o.portSet = true
			}
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) > 0 {
		agent, agentArgs = rest[0], rest[1:]
	}
	return
}

func interfaceIPs() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && !ip.IsUnspecified() {
				seen[ip.String()] = true
			}
		}
	}
	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips
}

// reorderPreferredFirst moves the LAN IP most likely reachable from a phone
// on the same network to the front of ips, in place.
func reorderPreferredFirst(ips []string) {
	if len(ips) < 2 {
		return
	}
	pref := preferredIP(ips)
	for i, ip := range ips {
		if ip == pref {
			ips[0], ips[i] = ips[i], ips[0]
			return
		}
	}
}

// preferredIP prefers the OS's default-route interface, else the ranges home
// routers actually hand out (192.168.x.x, then 10.x.x).
func preferredIP(ips []string) string {
	if out := outboundIP(); out != "" {
		for _, ip := range ips {
			if ip == out {
				return ip
			}
		}
	}
	for _, prefix := range []string{"192.168.", "10."} {
		for _, ip := range ips {
			if strings.HasPrefix(ip, prefix) {
				return ip
			}
		}
	}
	return ips[0]
}

// outboundIP returns the local address the OS would route through to reach
// the internet. UDP "connect" only consults the routing table — no packets
// sent — and 203.0.113.0/24 is the RFC 5737 documentation range.
func outboundIP() string {
	conn, err := net.Dial("udp4", "203.0.113.1:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return addr.IP.String()
}
