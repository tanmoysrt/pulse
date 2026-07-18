package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

func codexHookArgs(id, token string) []string {
	// Hook callbacks carry the token as a query param, same as the UI.
	q := ""
	if token != "" {
		q = "?t=" + token
	}
	hook := func(path string, timeoutSec int) string {
		cmd := fmt.Sprintf(`curl -sS --max-time %d -X POST http://127.0.0.1:$PULSE_PORT/hooks/%s/%s%s -H "Content-Type: application/json" --data-binary @-`, timeoutSec, id, path, q)
		q, _ := json.Marshal(cmd)
		return fmt.Sprintf(`[{hooks=[{type="command",command=%s,timeout=%d}]}]`, q, timeoutSec)
	}
	return []string{
		"-c", "hooks.SessionStart=" + hook("session-start", 30),
		"-c", "hooks.PermissionRequest=" + hook("permission", 900),
		"-c", "hooks.Stop=" + hook("stop", 30),
		"--dangerously-bypass-hook-trust",
	}
}

func parseCodexLine(lineNo int, raw []byte) parsed {
	var l struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &l) != nil {
		return parsed{}
	}
	if l.Type == "turn_context" {
		var tc struct {
			Model string `json:"model"`
		}
		json.Unmarshal(l.Payload, &tc)
		return parsed{model: tc.Model}
	}
	if l.Type != "response_item" {
		return parsed{}
	}
	var p struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		CallID    string `json:"call_id"`
		Output    string `json:"output"`
		Summary   []struct {
			Text string `json:"text"`
		} `json:"summary"`
	}
	if json.Unmarshal(l.Payload, &p) != nil {
		return parsed{}
	}
	line := lineNo * 10
	switch p.Type {
	case "message":
		if p.Role != "user" && p.Role != "assistant" {
			return parsed{}
		}
		var text strings.Builder
		for _, b := range p.Content {
			text.WriteString(b.Text)
		}
		s := truncate(stripANSI(text.String()), maxText)
		if s == "" || strings.HasPrefix(s, "<environment_context>") {
			return parsed{}
		}
		return parsed{msgs: []Message{{Line: line, Role: p.Role, Kind: "text", Text: s}}}
	case "reasoning":
		var text strings.Builder
		for _, s := range p.Summary {
			text.WriteString(s.Text)
		}
		if text.Len() == 0 {
			return parsed{}
		}
		return parsed{msgs: []Message{{Line: line, Role: "assistant", Kind: "thinking", Text: truncate(text.String(), maxText)}}}
	case "function_call":
		return parsed{msgs: []Message{{Line: line, Role: "assistant", Kind: "tool_use", Name: p.Name, Text: truncate(prettyJSON(json.RawMessage(p.Arguments)), maxText)}}}
	case "function_call_output":
		out := truncate(stripANSI(p.Output), maxText)
		if out == "" {
			return parsed{}
		}
		return parsed{msgs: []Message{{Line: line, Role: "assistant", Kind: "tool_result", Text: out, resultFor: p.CallID}}}
	}
	return parsed{}
}

func codexStatusLine(pane string) string {
	lines := strings.Split(strings.TrimRight(pane, "\n"), "\n")
	for i := len(lines) - 1; i >= 0 && i > len(lines)-6; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

var codexPlanModeRE = regexp.MustCompile(`Plan mode\s*$`)

func parseCodexMode(pane string) string {
	if codexPlanModeRE.MatchString(codexStatusLine(pane)) {
		return "plan"
	}
	return "default"
}

var codexModelEffortRE = regexp.MustCompile(`^\s*(\S+)\s+(low|medium|high|xhigh)\s*·`)

func parseCodexModelEffort(pane string) (model, effort string) {
	m := codexModelEffortRE.FindStringSubmatch(codexStatusLine(pane))
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

func codexSetMode(session, target string) {
	if parseCodexMode(tmuxCapture(session)) != target {
		tmuxSendKey(session, "BTab")
	}
}

var codexMenuItemRE = regexp.MustCompile(`(?m)^.\s*(\d+)\.\s+(\S+)`)

func codexMenuIndex(pane, needle string) string {
	needle = strings.ToLower(needle)
	for _, m := range codexMenuItemRE.FindAllStringSubmatch(pane, -1) {
		if strings.Contains(strings.ToLower(m[2]), needle) {
			return m[1]
		}
	}
	return ""
}

func codexSetModel(session, modelID string) {
	tmuxSendText(session, "/model")
	time.Sleep(700 * time.Millisecond)
	idx := codexMenuIndex(tmuxCapture(session), modelID)
	if idx == "" {
		tmuxSendKey(session, "Escape")
		return
	}
	tmuxSendKey(session, idx)
	time.Sleep(500 * time.Millisecond)
	tmuxSendKey(session, "Enter")
}

func codexEffortNeedle(level string) string {
	if level == "xhigh" {
		return "Extra"
	}
	return level
}

func codexSetEffort(session, level string) {
	tmuxSendText(session, "/model")
	time.Sleep(700 * time.Millisecond)
	tmuxSendKey(session, "Enter")
	time.Sleep(500 * time.Millisecond)
	idx := codexMenuIndex(tmuxCapture(session), codexEffortNeedle(level))
	if idx == "" {
		tmuxSendKey(session, "Escape")
		return
	}
	tmuxSendKey(session, idx)
	time.Sleep(300 * time.Millisecond)
	tmuxSendKey(session, "Enter")
}
