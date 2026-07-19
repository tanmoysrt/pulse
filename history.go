package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Surface transcripts touched within this window; older ones stay on disk but
// are hidden from the listing. Listing reads each file's head for a title.
const histWindow = 14 * 24 * time.Hour

// historyList gathers transcripts touched in the last histWindow from every
// installed agent, newest first.
func historyList() []listItem {
	cutoff := time.Now().Add(-histWindow).UnixMilli()
	var all []listItem
	all = append(all, fileHistory(filepath.Join(home(), ".claude", "projects", "*", "*.jsonl"), "claude", claudeMeta, cutoff)...)
	all = append(all, fileHistory(filepath.Join(home(), ".codex", "sessions", "*", "*", "*", "rollout-*.jsonl"), "codex", codexMeta, cutoff)...)
	all = append(all, opencodeHistory(cutoff)...)
	sort.Slice(all, func(i, j int) bool { return all[i].Updated > all[j].Updated })
	return all
}

// apiHistory returns one page of a transcript. Default is the last `limit`
// messages; ?before=<start> yields the page preceding that index.
func (d *Daemon) apiHistory(c echo.Context) error {
	tool, locator, ok := parseRef(c.QueryParam("ref"))
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "bad ref"})
	}
	msgs, err := historyMessages(tool, locator)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	total := len(msgs)
	limit := queryInt(c, "limit", 50)
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	end := queryInt(c, "before", total)
	if end < 0 {
		end = 0
	} else if end > total {
		end = total
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	page := msgs[start:end]
	if page == nil {
		page = []Message{}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"tool": tool, "total": total, "start": start, "end": end, "messages": page,
	})
}

func queryInt(c echo.Context, name string, def int) int {
	if v := c.QueryParam(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// resume describes how to relaunch a past session: the agent, its original
// directory, the CLI args that resume it, and where to read its transcript
// (a file path for claude/codex, or the session id for opencode).
type resume struct {
	agent, dir string
	args       []string
	transcript string
}

// resumeSpec resolves a history ref into a resume plan. Claude's SessionStart
// hook does not fire on resume, so callers adopt r.transcript directly.
func resumeSpec(ref string) (*resume, error) {
	tool, locator, ok := parseRef(ref)
	if !ok {
		return nil, fmt.Errorf("bad ref")
	}
	switch tool {
	case "claude":
		id := strings.TrimSuffix(filepath.Base(locator), ".jsonl")
		dir, _ := claudeMeta(locator)
		return &resume{"claude", dir, []string{"--resume", id}, locator}, nil
	case "codex":
		dir, id := codexResumeInfo(locator)
		if id == "" {
			return nil, fmt.Errorf("codex session id not found")
		}
		return &resume{"codex", dir, []string{"resume", id}, locator}, nil
	case "opencode":
		return &resume{"opencode", opencodeDir(locator), []string{"--session", locator}, locator}, nil
	}
	return nil, fmt.Errorf("unknown tool %q", tool)
}

func codexResumeInfo(path string) (dir, id string) {
	scanHead(path, 50, func(raw []byte) {
		var l struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &l) != nil || l.Type != "session_meta" {
			return
		}
		var p struct {
			Cwd       string `json:"cwd"`
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
		}
		json.Unmarshal(l.Payload, &p)
		dir, id = p.Cwd, p.SessionID
		if id == "" {
			id = p.ID
		}
	})
	return
}

func opencodeDir(id string) string {
	var rows []struct {
		Directory string `json:"directory"`
	}
	sqliteJSON(opencodeDBPath(), "SELECT directory FROM session WHERE id="+sqlQuote(id), &rows)
	if len(rows) > 0 {
		return rows[0].Directory
	}
	return ""
}

func historyMessages(tool, locator string) ([]Message, error) {
	switch tool {
	case "claude":
		return readFileMessages(locator, parseLine)
	case "codex":
		return readFileMessages(locator, parseCodexLine)
	case "opencode":
		return opencodeMessages(locator)
	}
	return nil, fmt.Errorf("unknown tool %q", tool)
}

func fileHistory(pattern, tool string, meta func(path string) (dir, title string), cutoff int64) []listItem {
	paths, _ := filepath.Glob(pattern)
	var out []listItem
	for _, p := range paths {
		updated := mtimeMs(p)
		if updated < cutoff {
			continue
		}
		dir, title := meta(p)
		out = append(out, listItem{
			ID: histRef(tool, p), Tool: tool, Dir: dir, Title: title, Updated: updated,
		})
	}
	return out
}

// claudeMeta reads dir + title from a transcript head (AI title, else first user line).
func claudeMeta(path string) (dir, title string) {
	firstUser := ""
	scanHead(path, 600, func(raw []byte) {
		var l struct {
			Type        string                            `json:"type"`
			Cwd         string                            `json:"cwd"`
			AiTitle     string                            `json:"aiTitle"`
			IsMeta      bool                              `json:"isMeta"`
			IsSidechain bool                              `json:"isSidechain"`
			Message     struct{ Content json.RawMessage } `json:"message"`
		}
		if json.Unmarshal(raw, &l) != nil {
			return
		}
		if l.Cwd != "" && dir == "" {
			dir = l.Cwd
		}
		if l.AiTitle != "" {
			title = l.AiTitle
		}
		if firstUser == "" && l.Type == "user" && !l.IsMeta && !l.IsSidechain {
			firstUser = firstText(l.Message.Content)
		}
	})
	if title == "" {
		title = firstUser
	}
	return dir, truncate(title, 120)
}

func codexMeta(path string) (dir, title string) {
	scanHead(path, 600, func(raw []byte) {
		var l struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &l) != nil {
			return
		}
		switch l.Type {
		case "session_meta":
			var p struct {
				Cwd string `json:"cwd"`
			}
			json.Unmarshal(l.Payload, &p)
			if p.Cwd != "" {
				dir = p.Cwd
			}
		case "response_item":
			if title != "" {
				return
			}
			var p struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			json.Unmarshal(l.Payload, &p)
			if p.Type == "message" && p.Role == "user" {
				for _, b := range p.Content {
					t := strings.TrimSpace(b.Text)
					if t != "" && !strings.HasPrefix(t, "<") {
						title = t
						return
					}
				}
			}
		}
	})
	return dir, truncate(title, 120)
}

// opencodeHistory reads sessions from OpenCode's SQLite store (no files on disk).
func opencodeHistory(cutoff int64) []listItem {
	db := opencodeDBPath()
	if _, err := os.Stat(db); err != nil {
		return nil
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil
	}
	var rows []struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
		Title     string `json:"title"`
		Updated   int64  `json:"time_updated"`
	}
	q := fmt.Sprintf("SELECT id, directory, title, time_updated FROM session WHERE time_archived IS NULL AND time_updated >= %d ORDER BY time_updated DESC", cutoff)
	if err := sqliteJSON(db, q, &rows); err != nil {
		return nil
	}
	out := make([]listItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, listItem{
			ID: histRef("opencode", r.ID), Tool: "opencode", Dir: r.Directory, Title: r.Title, Updated: r.Updated,
		})
	}
	return out
}

func opencodeMessages(sessionID string) ([]Message, error) {
	db := opencodeDBPath()
	var msgs []struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}
	if err := sqliteJSON(db, "SELECT id, data FROM message WHERE session_id="+sqlQuote(sessionID)+" ORDER BY time_created", &msgs); err != nil {
		return nil, err
	}
	var parts []struct {
		MessageID string `json:"message_id"`
		Data      string `json:"data"`
	}
	sqliteJSON(db, "SELECT message_id, data FROM part WHERE session_id="+sqlQuote(sessionID)+" ORDER BY time_created", &parts)
	byMsg := map[string][]string{}
	for _, p := range parts {
		byMsg[p.MessageID] = append(byMsg[p.MessageID], p.Data)
	}
	var out []Message
	n := 0
	for _, m := range msgs {
		var info struct {
			Role string `json:"role"`
		}
		json.Unmarshal([]byte(m.Data), &info)
		for _, pd := range byMsg[m.ID] {
			var p ocPart
			if json.Unmarshal([]byte(pd), &p) != nil {
				continue
			}
			for _, msg := range ocPartMessages(info.Role, p) {
				n++
				msg.Line = n
				out = append(out, msg)
			}
		}
	}
	return out, nil
}

// --- small helpers -------------------------------------------------------

func readFileMessages(path string, parse lineParser) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		out = append(out, parse(n, append([]byte(nil), sc.Bytes()...)).msgs...)
		n++
	}
	return out, nil
}

func scanHead(path string, maxLines int, fn func(raw []byte)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for i := 0; i < maxLines && sc.Scan(); i++ {
		fn(sc.Bytes())
	}
}

func firstText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return strings.TrimSpace(b.Text)
			}
		}
	}
	return ""
}

func sqliteJSON(db, query string, out any) error {
	b, err := exec.Command("sqlite3", "-json", db, query).Output()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil // no rows
	}
	return json.Unmarshal(b, out)
}

func sqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func opencodeDBPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(home(), ".local", "share")
	}
	return filepath.Join(base, "opencode", "opencode.db")
}

func histRef(tool, locator string) string { return tool + ":" + b64.EncodeToString([]byte(locator)) }

func parseRef(ref string) (tool, locator string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i < 0 {
		return "", "", false
	}
	b, err := b64.DecodeString(ref[i+1:])
	if err != nil {
		return "", "", false
	}
	return ref[:i], string(b), true
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func mtimeMs(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixMilli()
}
