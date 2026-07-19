package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type PermissionReq struct {
	ID        int             `json:"id"`
	ToolName  string          `json:"toolName"`
	ToolInput json.RawMessage `json:"toolInput"`
	decision  chan string
	extID     string
}

type sseEvent struct {
	event string
	id    string
	data  []byte
}

type Session struct {
	d                *Daemon
	id               string
	dir              string
	createdAt        time.Time
	lastActive       time.Time
	ctx              context.Context
	cancel           context.CancelFunc
	cleanup          func()
	closed           bool
	mu               sync.Mutex
	tmuxSession      string
	agent            string
	ocBase           string
	started          bool
	sessionID        string
	transcriptPath   string
	status           string
	title            string
	model            string
	modelName        string
	mode             string
	effort           string
	modelSwitchUntil time.Time
	todos            []Todo
	taskIDs          []string
	taskSeq          int
	hiddenTasks      map[string]bool
	pending          *PermissionReq
	nextPermID       int
	uploadSeq        int
	messages         []Message
	subs             map[chan sseEvent]struct{}
	tailCancel       context.CancelFunc
}

func newSession(d *Daemon, id, tmuxSession, agent, dir string) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		d:           d,
		id:          id,
		dir:         dir,
		createdAt:   time.Now(),
		lastActive:  time.Now(),
		ctx:         ctx,
		cancel:      cancel,
		cleanup:     func() {},
		tmuxSession: tmuxSession,
		agent:       agent,
		status:      "idle",
		subs:        map[chan sseEvent]struct{}{},
		hiddenTasks: map[string]bool{},
	}
}

func (s *Session) broadcast(ev sseEvent) {
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// watching reports whether any browser is connected to the SSE stream.
func (s *Session) watching() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

// stateSnapshot is the JSON shape pushed on every "state" SSE event.
type stateSnapshot struct {
	Status    string         `json:"status"`
	Title     string         `json:"title"`
	Pending   *PermissionReq `json:"pending"`
	Model     string         `json:"model"`
	ModelName string         `json:"modelName"`
	Mode      string         `json:"mode"`
	Effort    string         `json:"effort"`
	Todos     []Todo         `json:"todos"`
	Agent     string         `json:"agent"`
	Started   bool           `json:"started"`
}

func (s *Session) stateEvent() sseEvent {
	data, _ := json.Marshal(stateSnapshot{
		Status: s.status, Title: s.title, Pending: s.pending,
		Model: s.model, ModelName: s.modelName, Mode: s.mode, Effort: s.effort,
		Todos: s.todos, Agent: s.agent, Started: s.started,
	})
	return sseEvent{event: "state", data: data}
}

func messageEvent(m Message) sseEvent {
	data, _ := json.Marshal(m)
	return sseEvent{event: "message", id: strconv.Itoa(m.Line), data: data}
}

// markStarted flips the UI out of its boot state once the agent is launched.
func (s *Session) markStarted() {
	s.mu.Lock()
	s.started = true
	s.broadcast(s.stateEvent())
	s.mu.Unlock()
}

func (s *Session) setStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.broadcast(s.stateEvent())
}

func (s *Session) appendMessage(m Message) {
	s.mu.Lock()
	s.messages = append(s.messages, m)
	s.lastActive = time.Now()
	s.broadcast(messageEvent(m))
	s.mu.Unlock()
}

func (s *Session) setMeta(title, model string) {
	s.mu.Lock()
	changed := false
	if title != "" && title != s.title {
		s.title, changed = title, true
	}
	if model != "" && model != s.model {
		s.model, changed = model, true
	}
	if changed {
		s.broadcast(s.stateEvent())
	}
	s.mu.Unlock()
}

func (s *Session) opencodeAskPermission(extID, toolName string, toolInput json.RawMessage) {
	s.mu.Lock()
	s.nextPermID++
	s.pending = &PermissionReq{ID: s.nextPermID, ToolName: toolName, ToolInput: toolInput, extID: extID}
	s.status = "needs_approval"
	s.broadcast(s.stateEvent())
	s.mu.Unlock()
	s.notifyPermission(toolName)
}

func (s *Session) opencodeClearPermission(extID string) {
	s.mu.Lock()
	if s.pending != nil && s.pending.extID == extID {
		s.pending = nil
		s.status = "running"
		s.broadcast(s.stateEvent())
	}
	s.mu.Unlock()
}

func (s *Session) adoptSession(sessionID, transcriptPath string) {
	s.mu.Lock()
	changed := false
	if sessionID != "" && sessionID != s.sessionID {
		s.sessionID, changed = sessionID, true
	}
	if transcriptPath != "" && transcriptPath != s.transcriptPath {
		s.transcriptPath, changed = transcriptPath, true
		if s.tailCancel != nil {
			s.tailCancel()
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.tailCancel = cancel
		go tailTranscript(ctx, transcriptPath, s.onTranscriptLine)
	}
	s.mu.Unlock()
	if changed {
		go s.d.persist() // record the locator for restart reconciliation
	}
}

func (s *Session) hookSessionStart(c echo.Context) error {
	var in struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
	}
	if err := c.Bind(&in); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	s.adoptSession(in.SessionID, in.TranscriptPath)
	return c.NoContent(http.StatusOK)
}

func (s *Session) onTranscriptLine(lineNo int, raw []byte) {
	parse := lineParser(parseLine)
	if s.agent == "codex" {
		parse = parseCodexLine
	}
	p := parse(lineNo, raw)
	s.mu.Lock()
	changed := false
	if p.title != "" && p.title != s.title {
		s.title, changed = p.title, true
	}
	if p.model != "" && p.model != s.model {
		s.model, changed = p.model, true
	}
	if len(p.ops) > 0 {
		s.applyTaskOps(p.ops)
		changed = true
	}
	if changed {
		s.broadcast(s.stateEvent())
	}
	for _, m := range p.msgs {
		if m.Kind == "tool_result" && s.hiddenTasks[m.resultFor] {
			continue
		}
		s.messages = append(s.messages, m)
		s.broadcast(messageEvent(m))
	}
	if len(p.msgs) > 0 {
		s.lastActive = time.Now()
	}
	s.mu.Unlock()
}

func (s *Session) applyTaskOps(ops []taskOp) {
	find := func(id string) int {
		for i, t := range s.taskIDs {
			if t == id {
				return i
			}
		}
		return -1
	}
	for _, op := range ops {
		if op.toolID != "" {
			s.hiddenTasks[op.toolID] = true
		}
		switch op.kind {
		case "snapshot":
			s.todos = op.todos
			s.taskIDs = make([]string, len(op.todos))
		case "create":
			s.taskSeq++
			s.todos = append(s.todos, Todo{Content: op.content, Status: "pending"})
			s.taskIDs = append(s.taskIDs, strconv.Itoa(s.taskSeq))
		case "update":
			if i := find(op.id); i >= 0 {
				if op.status != "" {
					s.todos[i].Status = op.status
				}
				if op.content != "" {
					s.todos[i].Content = op.content
				}
			}
		case "delete":
			if i := find(op.id); i >= 0 {
				s.todos = append(s.todos[:i], s.todos[i+1:]...)
				s.taskIDs = append(s.taskIDs[:i], s.taskIDs[i+1:]...)
			}
		}
	}
}

func (s *Session) hookPermission(c echo.Context) error {
	var in struct {
		SessionID      string          `json:"session_id"`
		TranscriptPath string          `json:"transcript_path"`
		ToolName       string          `json:"tool_name"`
		ToolInput      json.RawMessage `json:"tool_input"`
	}
	if err := c.Bind(&in); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	s.adoptSession(in.SessionID, in.TranscriptPath)
	req := &PermissionReq{ToolName: in.ToolName, ToolInput: in.ToolInput, decision: make(chan string, 1)}
	s.mu.Lock()
	s.nextPermID++
	req.ID = s.nextPermID
	s.pending = req
	s.status = "needs_approval"
	s.broadcast(s.stateEvent())
	s.mu.Unlock()
	s.notifyPermission(in.ToolName)

	var behavior string
	select {
	case behavior = <-req.decision:
	case <-time.After(880 * time.Second):
		s.clearPending(req.ID, "idle")
		return c.NoContent(http.StatusRequestTimeout)
	case <-c.Request().Context().Done():
		s.clearPending(req.ID, "idle")
		return nil
	}
	s.clearPending(req.ID, "running")
	return c.JSON(http.StatusOK, map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision":      map[string]string{"behavior": behavior},
		},
	})
}

func (s *Session) clearPending(id int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil && s.pending.ID == id {
		s.pending = nil
		s.status = status
		s.broadcast(s.stateEvent())
	}
}

func (s *Session) hookStop(c echo.Context) error {
	var in struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
	}
	if err := c.Bind(&in); err == nil {
		s.adoptSession(in.SessionID, in.TranscriptPath)
	}
	s.setStatus("idle")
	s.notifyDone()
	return c.NoContent(http.StatusOK)
}

func (s *Session) apiEvents(c echo.Context) error {
	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	after := -1
	if v := c.Request().Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			after = n
		}
	}

	ch := make(chan sseEvent, 256)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	replay := make([]Message, 0, len(s.messages))
	for _, m := range s.messages {
		if m.Line > after {
			replay = append(replay, m)
		}
	}
	state := s.stateEvent()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	write := func(ev sseEvent) error {
		if ev.id != "" {
			fmt.Fprintf(w, "id: %s\n", ev.id)
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.event, ev.data)
		w.Flush()
		return nil
	}
	write(state)
	for _, m := range replay {
		write(messageEvent(m))
	}
	w.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-ch:
			write(ev)
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		}
	}
}

func (s *Session) apiSend(c echo.Context) error {
	var in struct {
		Text string `json:"text"`
	}
	if err := c.Bind(&in); err != nil || in.Text == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "text required"})
	}
	if err := tmuxSendText(s.tmuxSession, in.Text); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	s.setStatus("running")
	return c.NoContent(http.StatusOK)
}

func (s *Session) apiPermission(c echo.Context) error {
	var in struct {
		ID       int    `json:"id"`
		Decision string `json:"decision"`
	}
	if err := c.Bind(&in); err != nil || (in.Decision != "allow" && in.Decision != "deny") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "decision must be allow or deny"})
	}
	s.mu.Lock()
	req, agent, ocBase := s.pending, s.agent, s.ocBase
	s.mu.Unlock()
	if req == nil || req.ID != in.ID {
		return c.JSON(http.StatusConflict, map[string]string{"error": "no matching pending permission"})
	}
	if agent == "opencode" {
		reply := "once"
		if in.Decision == "deny" {
			reply = "reject"
		}
		go ocReply(ocBase, req.extID, reply)
		return c.NoContent(http.StatusOK)
	}
	select {
	case req.decision <- in.Decision:
	default:
	}
	return c.NoContent(http.StatusOK)
}

func (s *Session) apiInterrupt(c echo.Context) error {
	if err := tmuxSendKey(s.tmuxSession, "Escape"); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	s.setStatus("idle")
	return c.NoContent(http.StatusOK)
}

// apiClose ends this agent session (kills its tmux, stops its goroutines) and
// unregisters it from the daemon, which keeps running.
func (s *Session) apiClose(c echo.Context) error {
	s.d.remove(s.id)
	return c.NoContent(http.StatusOK)
}

// uploadDir stages attached files. Every agent CLI reads a plain absolute path
// typed into the prompt, so the UI just needs somewhere to drop the upload.
func (s *Session) uploadDir() string {
	return filepath.Join(os.TempDir(), "pulse-uploads-"+s.tmuxSession)
}

var safeUploadExt = regexp.MustCompile(`^\.[A-Za-z0-9]{1,10}$`)

const maxUploadSize = 20 << 20 // 20MB

func (s *Session) apiUpload(c echo.Context) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file required"})
	}
	if fh.Size > maxUploadSize {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large (max 20MB)"})
	}
	src, err := fh.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	defer src.Close()

	dir := s.uploadDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	// Name the file ourselves (client names are untrusted), keeping the
	// extension so the agent's file-type sniffing still works.
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !safeUploadExt.MatchString(ext) {
		ext = ""
	}
	s.mu.Lock()
	s.uploadSeq++
	dst := filepath.Join(dir, fmt.Sprintf("upload-%d%s", s.uploadSeq, ext))
	s.mu.Unlock()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"path": dst, "name": fh.Filename})
}

// clearCommands is each agent's "new conversation" slash command.
var clearCommands = map[string]string{"claude": "/clear", "codex": "/clear", "opencode": "/new"}

// apiClear clears the agent's context and wipes pulse's replay buffer so the
// old history doesn't reappear on reconnect.
func (s *Session) apiClear(c echo.Context) error {
	if cmd := clearCommands[s.agent]; cmd != "" {
		if err := tmuxSendText(s.tmuxSession, cmd); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	s.mu.Lock()
	s.messages = nil
	s.todos = nil
	s.taskIDs = nil
	s.taskSeq = 0
	s.hiddenTasks = map[string]bool{}
	s.broadcast(sseEvent{event: "cleared", data: []byte("{}")})
	s.mu.Unlock()
	return c.NoContent(http.StatusOK)
}

// apiCompact asks the agent to summarize and shrink its context. Unlike clear,
// pulse's transcript is kept — the agent posts a compacted summary inline.
func (s *Session) apiCompact(c echo.Context) error {
	if err := tmuxSendText(s.tmuxSession, "/compact"); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusOK)
}

var modelAliases = map[string]bool{
	"default": true, "opus": true, "sonnet": true, "haiku": true, "opusplan": true,
}

func (s *Session) apiModel(c echo.Context) error {
	var in struct {
		Model string `json:"model"`
		Label string `json:"label"`
	}
	if err := c.Bind(&in); err != nil || in.Model == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "model required"})
	}
	switch s.agent {
	case "codex":
		s.beginModelSwitch(in.Label)
		go codexSetModel(s.tmuxSession, in.Model)
	case "opencode":
		s.beginModelSwitch(in.Label)
		go opencodeSetModel(s.tmuxSession, s.ocBase, in.Model)
	default:
		if !modelAliases[in.Model] {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown model"})
		}
		if err := tmuxSendText(s.tmuxSession, "/model "+in.Model); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		s.confirmDialog()
	}
	return c.NoContent(http.StatusOK)
}

func (s *Session) holdChips() {
	s.mu.Lock()
	s.modelSwitchUntil = time.Now().Add(3 * time.Second)
	s.mu.Unlock()
}

func (s *Session) beginModelSwitch(label string) {
	s.holdChips()
	if label != "" {
		s.mu.Lock()
		s.model, s.modelName = "", label
		s.broadcast(s.stateEvent())
		s.mu.Unlock()
	}
}

func (s *Session) apiModels(c echo.Context) error {
	models := []map[string]string{}
	if s.agent == "opencode" {
		if m := opencodeModels(s.ocBase); m != nil {
			models = m
		}
	}
	return c.JSON(http.StatusOK, models)
}

func (s *Session) confirmDialog() {
	go func() {
		time.Sleep(700 * time.Millisecond)
		tmuxSendKey(s.tmuxSession, "Enter")
	}()
}

var effortLevels = map[string]bool{
	"low": true, "medium": true, "high": true, "xhigh": true, "max": true,
}

func (s *Session) apiEffort(c echo.Context) error {
	var in struct {
		Level string `json:"level"`
	}
	if err := c.Bind(&in); err != nil || !effortLevels[in.Level] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown effort level"})
	}
	switch s.agent {
	case "codex":
		s.holdChips()
		go codexSetEffort(s.tmuxSession, in.Level)
	case "opencode":
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "opencode has no effort control"})
	default:
		if err := tmuxSendText(s.tmuxSession, "/effort "+in.Level); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		s.confirmDialog()
	}
	return c.NoContent(http.StatusOK)
}

var modeOrder = []string{"manual", "acceptEdits", "plan", "auto"}

func modeIndex(m string) int {
	for i, v := range modeOrder {
		if v == m {
			return i
		}
	}
	return -1
}

func (s *Session) apiMode(c echo.Context) error {
	var in struct {
		Mode string `json:"mode"`
	}
	if err := c.Bind(&in); err != nil || in.Mode == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "mode required"})
	}
	switch s.agent {
	case "codex":
		codexSetMode(s.tmuxSession, in.Mode)
	case "opencode":
		opencodeSetMode(s.tmuxSession, in.Mode)
	default:
		if modeIndex(in.Mode) < 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown mode"})
		}
		cur := modeIndex(parseMode(tmuxCapture(s.tmuxSession)))
		if cur < 0 {
			return c.JSON(http.StatusConflict, map[string]string{"error": "current mode unknown"})
		}
		for n := (modeIndex(in.Mode) - cur + len(modeOrder)) % len(modeOrder); n > 0; n-- {
			tmuxSendKey(s.tmuxSession, "BTab")
			time.Sleep(120 * time.Millisecond)
		}
	}
	s.mu.Lock()
	s.mode = in.Mode
	s.broadcast(s.stateEvent())
	s.mu.Unlock()
	return c.NoContent(http.StatusOK)
}

func (s *Session) pollMode() {
	parseCurMode := parseMode
	switch s.agent {
	case "codex":
		parseCurMode = parseCodexMode
	case "opencode":
		parseCurMode = parseOpencodeMode
	}
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		// The agent exited (or its session was killed): reap and stop.
		if !tmuxAlive(s.tmuxSession) {
			go s.d.remove(s.id)
			return
		}
		// No one is watching the UI, so skip the tmux subprocess entirely.
		if !s.watching() {
			continue
		}
		pane := tmuxCapture(s.tmuxSession)
		m := parseCurMode(pane)
		s.mu.Lock()
		changed := false
		if m != "" && m != s.mode {
			s.mode, changed = m, true
		}
		switch s.agent {
		case "claude":
			if e := parseEffort(pane); e != "" && e != s.effort {
				s.effort, changed = e, true
			}
			if name := parseModelName(pane); name != "" && name != s.modelName {
				s.modelName, changed = name, true
			}
		case "codex":
			if time.Now().After(s.modelSwitchUntil) {
				if model, e := parseCodexModelEffort(pane); model != "" || e != "" {
					if model != "" && model != s.modelName {
						s.modelName, changed = model, true
					}
					if e != "" && e != s.effort {
						s.effort, changed = e, true
					}
				}
			}
		case "opencode":
			if time.Now().After(s.modelSwitchUntil) {
				if name := opencodeModelName(pane, s.ocBase); name != "" && name != s.modelName {
					s.modelName, changed = name, true
				}
			}
		}
		if changed {
			s.broadcast(s.stateEvent())
		}
		s.mu.Unlock()
	}
}
