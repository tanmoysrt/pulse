package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
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
}

// shutdown ends every session; called when the daemon exits.
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
	return s, nil
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
	return c.JSON(http.StatusOK, map[string]any{"live": live, "history": historyList()})
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
		path, _ = os.UserHomeDir()
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
	g.POST("/model", d.withSession((*Session).apiModel))
	g.GET("/models", d.withSession((*Session).apiModels))
	g.POST("/effort", d.withSession((*Session).apiEffort))
	g.POST("/mode", d.withSession((*Session).apiMode))

	hk := e.Group("/hooks/:id")
	hk.POST("/session-start", d.withSession((*Session).hookSessionStart))
	hk.POST("/permission", d.withSession((*Session).hookPermission))
	hk.POST("/stop", d.withSession((*Session).hookStop))

	e.GET("/", func(c echo.Context) error { return c.HTMLBlob(http.StatusOK, indexHTML) })
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
