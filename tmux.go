package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func tmuxRun(session string, env, args []string) error {
	go tmuxHideChrome(session)
	headless := func() bool {
		fi, err := os.Stdout.Stat()
		return err != nil || fi.Mode()&os.ModeCharDevice == 0
	}()
	flags := []string{"-2", "new-session", "-s", session}
	if headless {
		flags = append(flags, "-d")
	}
	for _, e := range env {
		flags = append(flags, "-e", e)
	}
	flags = append(flags, args...)
	if headless {
		if err := exec.Command("tmux", flags...).Run(); err != nil {
			return err
		}
		for exec.Command("tmux", "has-session", "-t", session).Run() == nil {
			time.Sleep(time.Second)
		}
		return nil
	}
	cmd := exec.Command("tmux", flags...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func tmuxHideChrome(session string) {
	for range 50 {
		if exec.Command("tmux", "has-session", "-t", session).Run() == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, opt := range [][2]string{{"status", "off"}, {"prefix", "None"}, {"prefix2", "None"}, {"focus-events", "on"}} {
		exec.Command("tmux", "set-option", "-t", session, opt[0], opt[1]).Run()
	}
	exec.Command("tmux", "set-option", "-ga", "terminal-overrides", ",*:Tc").Run()
	exec.Command("tmux", "set-option", "-as", "terminal-features", ",*:RGB").Run()
	// bind-key requires a live server, which only exists once the session
	// above has actually been created (tmux doesn't auto-start one for it).
	tmuxBindLinksKey()
}

func tmuxSendKey(session, key string) error {
	return exec.Command("tmux", "send-keys", "-t", session, key).Run()
}

func tmuxKill(session string) error {
	return exec.Command("tmux", "kill-session", "-t", session).Run()
}

// linksHotkey is the unprefixed key bound (server-wide) to pop up the
// session's URLs/QR again. Chosen deliberately obscure (rarely bound by
// shells, editors, or the agent CLIs) since -n bindings apply to every window
// on the tmux server, not just pulse's own.
const linksHotkey = "F12"

func linksPath(session string) string {
	return filepath.Join(os.TempDir(), "pulse-links-"+session+".txt")
}

// tmuxWriteLinks stashes the startup banner (URLs + QR) to a per-session file
// so it can be redisplayed later without rerunning pulse or leaving the agent.
func tmuxWriteLinks(session, content string) error {
	return os.WriteFile(linksPath(session), []byte(content), 0o600)
}

// tmuxBindLinksKey registers a single global popup binding whose command
// resolves the right file at press-time by asking tmux for the current
// client's session name itself (`tmux display-message -p '#S'`) — the
// popup's own shell inherits $TMUX from whichever client/session triggered
// it, so this always names the right file. (display-popup's command string
// is not itself format-expanded — #{session_name} there stays literal — so
// this indirection is required, not just stylistic.) Safe to call from every
// pulse process: each call just re-registers the same generic command, so
// concurrent pulse sessions on the shared tmux server don't stomp on each
// other's URL.
func tmuxBindLinksKey() {
	dir := os.TempDir()
	cmd := fmt.Sprintf(`sess=$(tmux display-message -p '#S'); cat '%s/pulse-links-'"$sess"'.txt' 2>/dev/null || echo "(pulse: no link info for this session)"; printf '\nPress any key to close\n'; read -n 1`, dir)
	exec.Command("tmux", "bind-key", "-n", linksHotkey,
		"display-popup", "-E", "-w", "90%", "-h", "90%", "-T", " pulse ", cmd).Run()
}

func tmuxCapture(session string) string {
	out, _ := exec.Command("tmux", "capture-pane", "-t", session, "-p").Output()
	return string(out)
}

var effortRE = regexp.MustCompile(`(low|medium|high|xhigh|max|ultracode)[^a-z\n]{0,6}/effort`)

func parseEffort(pane string) string {
	if m := effortRE.FindStringSubmatch(pane); m != nil {
		return m[1]
	}
	return ""
}

var modelRE = regexp.MustCompile(`([A-Za-z0-9.()  ]+?) with (?:low|medium|high|xhigh|max|ultracode) effort`)

func parseModelName(pane string) string {
	if m := modelRE.FindStringSubmatch(pane); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func parseMode(pane string) string {
	switch {
	case strings.Contains(pane, "accept edits on"):
		return "acceptEdits"
	case strings.Contains(pane, "plan mode on"):
		return "plan"
	case strings.Contains(pane, "auto mode on"):
		return "auto"
	case strings.Contains(pane, "manual mode on"):
		return "manual"
	}
	return ""
}

func tmuxSendText(session, text string) error {
	if err := exec.Command("tmux", "send-keys", "-t", session, "-l", text).Run(); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	return exec.Command("tmux", "send-keys", "-t", session, "Enter").Run()
}
