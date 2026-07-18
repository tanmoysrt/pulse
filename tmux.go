package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// tmuxSpawn creates a detached session running args in dir; clients attach separately.
func tmuxSpawn(session, dir string, env, args []string) error {
	flags := []string{"-2", "new-session", "-d", "-s", session}
	if dir != "" {
		flags = append(flags, "-c", dir)
	}
	for _, e := range env {
		flags = append(flags, "-e", e)
	}
	flags = append(flags, args...)
	if err := exec.Command("tmux", flags...).Run(); err != nil {
		return err
	}
	go tmuxHideChrome(session)
	return nil
}

// tmuxAttach connects the current terminal to an existing session.
func tmuxAttach(session string) error {
	cmd := exec.Command("tmux", "attach", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func tmuxAlive(session string) bool {
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
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
}

func tmuxSendKey(session, key string) error {
	return exec.Command("tmux", "send-keys", "-t", session, key).Run()
}

func tmuxKill(session string) error {
	return exec.Command("tmux", "kill-session", "-t", session).Run()
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
