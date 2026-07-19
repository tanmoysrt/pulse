package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Daemon owns every live Session and serves the UI plus cross-tool history.
type Daemon struct {
	mu       sync.Mutex
	sessions map[string]*Session
	seq      int
	token    string
	quiet    bool
	vapid    *vapidKey
	pushSubs []pushSub
	port     int
}

func newDaemon(token string, quiet bool, port int) *Daemon {
	return &Daemon{
		sessions: map[string]*Session{},
		token:    token,
		quiet:    quiet,
		port:     port,
		vapid:    loadOrCreateVapid(),
	}
}

func (d *Daemon) get(id string) *Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[id]
}

// remove ends a session (stops goroutines, kills tmux) and drops it. The
// daemon keeps running.
func (d *Daemon) remove(id string) {
	d.mu.Lock()
	s := d.sessions[id]
	delete(d.sessions, id)
	d.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.broadcast(sseEvent{event: "closed", data: []byte("{}")})
	s.mu.Unlock()
	s.cancel()
	tmuxKill(s.tmuxSession)
	s.cleanup()
	os.RemoveAll(s.uploadDir())
	d.persist()
}

// shutdown ends every session (killing their tmux) and clears all state;
// called when the user chooses to stop everything.
func (d *Daemon) shutdown() {
	d.mu.Lock()
	ids := make([]string, 0, len(d.sessions))
	for id := range d.sessions {
		ids = append(ids, id)
	}
	d.mu.Unlock()
	for _, id := range ids {
		d.remove(id)
	}
	removeState()
	removeSessions()
}

// detach stops the daemon but leaves every tmux session running; a later
// restart reconciles them from the persisted registry.
func (d *Daemon) detach() {
	d.persist()
	removeState()
}

func (d *Daemon) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sessions)
}

// spawn launches an agent in a detached tmux session wired to per-session
// hooks, then registers it. When r is set the session is resumed: its known
// transcript is adopted directly (claude's SessionStart hook won't fire).
func (d *Daemon) spawn(agent, dir string, agentArgs []string, r *resume) (*Session, error) {
	d.mu.Lock()
	d.seq++
	id := strconv.Itoa(d.seq)
	d.mu.Unlock()

	tmuxSession := "pulse-" + id
	s := newSession(d, id, tmuxSession, agent, dir)

	var extraArgs []string
	switch agent {
	case "claude":
		settings, err := hookSettings(d.port, id, d.token)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(os.TempDir(), fmt.Sprintf("pulse-settings-%s.json", id))
		if err := os.WriteFile(path, settings, 0o600); err != nil {
			return nil, err
		}
		extraArgs = []string{"--settings", path}
		s.cleanup = func() { os.Remove(path) }
	case "codex":
		extraArgs = codexHookArgs(id, d.token)
	case "opencode":
		ocLn, ocPort := freePort("127.0.0.1")
		ocLn.Close()
		s.ocBase = fmt.Sprintf("http://127.0.0.1:%d", ocPort)
		extraArgs = []string{"--port", strconv.Itoa(ocPort), "--hostname", "127.0.0.1"}
	}

	args := append(append([]string{}, agentArgs...), extraArgs...)
	env := []string{fmt.Sprintf("PULSE_PORT=%d", d.port)}
	if err := tmuxSpawn(tmuxSession, dir, env, append([]string{agent}, args...)); err != nil {
		s.cleanup()
		return nil, err
	}

	d.mu.Lock()
	d.sessions[id] = s
	d.mu.Unlock()

	s.markStarted()
	if r != nil && (agent == "claude" || agent == "codex") {
		s.adoptSession("", r.transcript)
	}
	go s.pollMode()
	if agent == "opencode" {
		knownID := ""
		if r != nil {
			knownID = r.transcript
		}
		go opencodePoll(s.ctx, s, s.ocBase, dir, time.Now(), knownID)
	}
	d.persist()
	return s, nil
}

// sessionRecord is a persisted hint about a live session. tmux is the source of
// truth: on restart a record whose tmux session is gone is discarded.
type sessionRecord struct {
	ID         string `json:"id"`
	Agent      string `json:"agent"`
	Dir        string `json:"dir"`
	Tmux       string `json:"tmux"`
	Transcript string `json:"transcript,omitempty"`
	SessionID  string `json:"sessionID,omitempty"`
	OCBase     string `json:"ocBase,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
}

// persist snapshots the live sessions to disk so a restarted daemon can adopt
// any that are still running in tmux.
func (d *Daemon) persist() {
	d.mu.Lock()
	recs := make([]sessionRecord, 0, len(d.sessions))
	for _, s := range d.sessions {
		s.mu.Lock()
		recs = append(recs, sessionRecord{
			ID: s.id, Agent: s.agent, Dir: s.dir, Tmux: s.tmuxSession,
			Transcript: s.transcriptPath, SessionID: s.sessionID, OCBase: s.ocBase,
			CreatedAt: s.createdAt.UnixMilli(),
		})
		s.mu.Unlock()
	}
	d.mu.Unlock()
	writeSessions(recs)
}

// reconcile re-adopts tmux sessions recorded before a restart that are still
// alive, and prunes the rest. It trusts tmux, not the file.
func (d *Daemon) reconcile() {
	recs, _ := readSessions()
	maxSeq := 0
	for _, r := range recs {
		if n, err := strconv.Atoi(r.ID); err == nil && n > maxSeq {
			maxSeq = n
		}
		if !tmuxAlive(r.Tmux) {
			continue
		}
		s := newSession(d, r.ID, r.Tmux, r.Agent, r.Dir)
		s.ocBase = r.OCBase
		if r.CreatedAt > 0 {
			s.createdAt = time.UnixMilli(r.CreatedAt)
		}
		d.mu.Lock()
		d.sessions[r.ID] = s
		d.mu.Unlock()
		s.markStarted()
		go s.pollMode()
		if r.Agent == "opencode" {
			go opencodePoll(s.ctx, s, s.ocBase, r.Dir, s.createdAt, r.SessionID)
		} else if r.Transcript != "" {
			s.adoptSession(r.SessionID, r.Transcript)
		}
	}
	d.mu.Lock()
	if maxSeq > d.seq {
		d.seq = maxSeq
	}
	n := len(d.sessions)
	d.mu.Unlock()
	d.persist()
	if n > 0 {
		fmt.Printf("pulse: reconciled %d running session(s)\n", n)
	}
}

// listItem is one row in the UI list: a live session or a past transcript.
type listItem struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Dir     string `json:"dir"`
	Title   string `json:"title"`
	Status  string `json:"status,omitempty"`
	Live    bool   `json:"live"`
	Updated int64  `json:"updated"`
}

func (d *Daemon) apiList(c echo.Context) error {
	d.mu.Lock()
	live := make([]listItem, 0, len(d.sessions))
	for _, s := range d.sessions {
		s.mu.Lock()
		live = append(live, listItem{
			ID: s.id, Tool: s.agent, Dir: s.dir, Title: s.title,
			Status: s.status, Live: true, Updated: s.createdAt.UnixMilli(),
		})
		s.mu.Unlock()
	}
	d.mu.Unlock()
	sort.Slice(live, func(i, j int) bool { return live[i].Updated > live[j].Updated })
	return c.JSON(http.StatusOK, map[string]any{"live": live, "history": historyList(), "installed": installedAgents()})
}

// installedAgents lists the agents whose CLI is on PATH, newest-first order.
func installedAgents() []string {
	out := []string{}
	for _, a := range []string{"claude", "codex", "opencode"} {
		if _, err := exec.LookPath(a); err == nil {
			out = append(out, a)
		}
	}
	return out
}

func (d *Daemon) apiSpawn(c echo.Context) error {
	var in struct {
		Agent  string   `json:"agent"`
		Dir    string   `json:"dir"`
		Args   []string `json:"args"`
		Resume string   `json:"resume"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "bad request"})
	}
	agent, dir, args := in.Agent, in.Dir, in.Args
	var r *resume
	if in.Resume != "" {
		var err error
		if r, err = resumeSpec(in.Resume); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		agent, dir, args = r.agent, r.dir, r.args
	}
	if !validAgents[agent] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "valid agent required"})
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no such directory: " + dir})
	}
	s, err := d.spawn(agent, dir, args, r)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"id": s.id, "tmux": s.tmuxSession, "agent": agent})
}

// apiDirs lists subdirectories of ?path (default home) for the new-chat picker.
func (d *Daemon) apiDirs(c echo.Context) error {
	path := c.QueryParam("path")
	if path == "" {
		path, _ = os.Getwd() // default to where the daemon was started
	}
	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	dirs := []string{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return c.JSON(http.StatusOK, map[string]any{"path": path, "parent": filepath.Dir(path), "dirs": dirs})
}

// withSession resolves the :id path param to a live session.
func (d *Daemon) withSession(h func(*Session, echo.Context) error) echo.HandlerFunc {
	return func(c echo.Context) error {
		s := d.get(c.Param("id"))
		if s == nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "no such session"})
		}
		return h(s, c)
	}
}

func startServer(d *Daemon, ln net.Listener) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	if d.token != "" {
		e.Use(authMiddleware(d.token))
	}
	e.Use(middleware.BodyLimit(fmt.Sprintf("%dM", maxUploadSize/(1<<20)+1)))
	if os.Getenv("PULSE_DEBUG") != "" {
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				err := next(c)
				fmt.Printf("pulse: %s %s -> %d\n", c.Request().Method, c.Request().URL.Path, c.Response().Status)
				return err
			}
		})
	}

	e.GET("/api/sessions", d.apiList)
	e.POST("/api/sessions", d.apiSpawn)
	e.GET("/api/history", d.apiHistory)
	e.GET("/api/dirs", d.apiDirs)
	e.GET("/api/push/key", d.apiPushKey)
	e.POST("/api/push/subscribe", d.apiPushSubscribe)

	g := e.Group("/api/sessions/:id")
	g.GET("/events", d.withSession((*Session).apiEvents))
	g.POST("/send", d.withSession((*Session).apiSend))
	g.POST("/upload", d.withSession((*Session).apiUpload))
	g.POST("/permission", d.withSession((*Session).apiPermission))
	g.POST("/interrupt", d.withSession((*Session).apiInterrupt))
	g.POST("/close", d.withSession((*Session).apiClose))
	g.POST("/clear", d.withSession((*Session).apiClear))
	g.POST("/compact", d.withSession((*Session).apiCompact))
	g.POST("/model", d.withSession((*Session).apiModel))
	g.GET("/models", d.withSession((*Session).apiModels))
	g.POST("/effort", d.withSession((*Session).apiEffort))
	g.POST("/mode", d.withSession((*Session).apiMode))

	hk := e.Group("/hooks/:id")
	hk.POST("/session-start", d.withSession((*Session).hookSessionStart))
	hk.POST("/permission", d.withSession((*Session).hookPermission))
	hk.POST("/stop", d.withSession((*Session).hookStop))

	// The bundle changes every build, so revalidate (no-cache + ETag) rather
	// than cache blindly: unchanged fetches get a bodyless 304, new builds load.
	indexETag := fmt.Sprintf(`"%x"`, sha256.Sum256(indexHTML))
	e.GET("/", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("ETag", indexETag)
		if c.Request().Header.Get("If-None-Match") == indexETag {
			return c.NoContent(http.StatusNotModified)
		}
		return c.HTMLBlob(http.StatusOK, indexHTML)
	})
	e.GET("/sw.js", func(c echo.Context) error { return c.Blob(http.StatusOK, "application/javascript", swJS) })
	e.GET("/manifest.webmanifest", func(c echo.Context) error { return c.Blob(http.StatusOK, "application/manifest+json", manifestJSON) })

	e.Listener = ln
	go func() {
		if err := e.Start(""); err != nil && err != http.ErrServerClosed {
			fmt.Println("pulse: server error:", err)
		}
	}()
}

// daemonState lets a `pulse <agent>` client find the daemon's port and token.
type daemonState struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func statePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "pulse", "daemon.json")
}

func writeState(st daemonState) {
	b, _ := json.Marshal(st)
	os.MkdirAll(filepath.Dir(statePath()), 0o700)
	os.WriteFile(statePath(), b, 0o600)
}

func readState() (*daemonState, error) {
	b, err := os.ReadFile(statePath())
	if err != nil {
		return nil, err
	}
	var st daemonState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func removeState() { os.Remove(statePath()) }

func sessionsPath() string { return filepath.Join(filepath.Dir(statePath()), "sessions.json") }

func writeSessions(recs []sessionRecord) {
	b, _ := json.Marshal(recs)
	os.MkdirAll(filepath.Dir(sessionsPath()), 0o700)
	os.WriteFile(sessionsPath(), b, 0o600)
}

func readSessions() ([]sessionRecord, error) {
	b, err := os.ReadFile(sessionsPath())
	if err != nil {
		return nil, err
	}
	var recs []sessionRecord
	return recs, json.Unmarshal(b, &recs)
}

func removeSessions() { os.Remove(sessionsPath()) }
