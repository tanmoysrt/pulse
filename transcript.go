package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

type Message struct {
	Line int    `json:"line"`
	Role string `json:"role"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Text string `json:"text"`

	resultFor string
}

type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// parsed is the result of turning one transcript line into UI updates.
type parsed struct {
	msgs  []Message
	model string
	title string
	ops   []taskOp
}

// lineParser normalizes one raw transcript line; each agent has its own.
type lineParser func(lineNo int, raw []byte) parsed

type taskOp struct {
	kind    string
	todos   []Todo
	id      string
	content string
	status  string
	toolID  string
}

type transcriptLine struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	AiTitle     string `json:"aiTitle"`
	Message     struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

const maxText = 4000

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + " …"
	}
	return s
}

var (
	ansiRE    = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	cmdNameRE = regexp.MustCompile(`<command-name>([^<]*)</command-name>`)
	cmdArgsRE = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
	cmdOutRE  = regexp.MustCompile(`(?s)<local-command-stdout>(.*)</local-command-stdout>`)
)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func parseCommand(s string) (string, bool) {
	if m := cmdNameRE.FindStringSubmatch(s); m != nil {
		cmd := strings.TrimSpace(m[1])
		if a := cmdArgsRE.FindStringSubmatch(s); a != nil && strings.TrimSpace(a[1]) != "" {
			cmd += " " + strings.TrimSpace(a[1])
		}
		return cmd, true
	}
	if m := cmdOutRE.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1]), true
	}
	return "", false
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var t strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				t.WriteString(b.Text)
			}
		}
		return t.String()
	}
	return ""
}

func taskFromTool(name string, input json.RawMessage) (taskOp, bool) {
	switch name {
	case "TodoWrite":
		var td struct {
			Todos []Todo `json:"todos"`
		}
		json.Unmarshal(input, &td)
		return taskOp{kind: "snapshot", todos: td.Todos}, true
	case "TaskCreate":
		var t struct{ Subject, Description string }
		json.Unmarshal(input, &t)
		c := t.Subject
		if c == "" {
			c = t.Description
		}
		return taskOp{kind: "create", content: c}, true
	case "TaskUpdate":
		var t struct {
			TaskID  string `json:"taskId"`
			Status  string `json:"status"`
			Subject string `json:"subject"`
		}
		json.Unmarshal(input, &t)
		return taskOp{kind: "update", id: t.TaskID, status: t.Status, content: t.Subject}, true
	case "TaskDelete":
		var t struct {
			TaskID string `json:"taskId"`
		}
		json.Unmarshal(input, &t)
		return taskOp{kind: "delete", id: t.TaskID}, true
	}
	return taskOp{}, false
}

func parseLine(lineNo int, raw []byte) parsed {
	var tl transcriptLine
	if err := json.Unmarshal(raw, &tl); err != nil {
		return parsed{}
	}
	if tl.Type == "ai-title" {
		return parsed{title: tl.AiTitle}
	}
	if (tl.Type != "user" && tl.Type != "assistant") || tl.IsMeta || tl.IsSidechain {
		return parsed{}
	}
	role, model := tl.Message.Role, tl.Message.Model

	var s string
	if err := json.Unmarshal(tl.Message.Content, &s); err == nil {
		s = stripANSI(s)
		if cmd, ok := parseCommand(s); ok {
			return parsed{msgs: []Message{{Line: lineNo * 100, Role: role, Kind: "command", Text: truncate(cmd, maxText)}}, model: model}
		}
		s = truncate(s, maxText)
		if s == "" {
			return parsed{model: model}
		}
		return parsed{msgs: []Message{{Line: lineNo * 100, Role: role, Kind: "text", Text: s}}, model: model}
	}
	var blocks []contentBlock
	if err := json.Unmarshal(tl.Message.Content, &blocks); err != nil {
		return parsed{model: model}
	}

	var out []Message
	var ops []taskOp
	for i, b := range blocks {
		m := Message{Line: lineNo*100 + i, Role: role}
		switch b.Type {
		case "text":
			if b.Text == "" {
				continue
			}
			m.Kind, m.Text = "text", truncate(stripANSI(b.Text), maxText)
		case "thinking":
			if b.Thinking == "" {
				continue
			}
			m.Kind, m.Text = "thinking", truncate(stripANSI(b.Thinking), maxText)
		case "tool_use":
			if op, ok := taskFromTool(b.Name, b.Input); ok {
				op.toolID = b.ID
				ops = append(ops, op)
				continue
			}
			m.Kind, m.Name, m.Text = "tool_use", b.Name, truncate(prettyJSON(b.Input), maxText)
		case "tool_result":
			t := truncate(stripANSI(resultText(b.Content)), maxText)
			if t == "" {
				continue
			}
			m.Kind, m.Text, m.resultFor = "tool_result", t, b.ToolUseID
		default:
			continue
		}
		out = append(out, m)
	}
	return parsed{msgs: out, model: model, ops: ops}
}

func tailTranscript(ctx context.Context, path string, emit func(lineNo int, raw []byte)) {
	var offset int64
	lineNo := 0
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		f, err := os.Open(path)
		if err == nil {
			if _, err = f.Seek(offset, io.SeekStart); err == nil {
				r := bufio.NewReader(f)
				for {
					raw, err := r.ReadBytes('\n')
					if err != nil {
						break
					}
					offset += int64(len(raw))
					emit(lineNo, raw)
					lineNo++
				}
			}
			f.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
