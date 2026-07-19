package main

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type ocSession struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Time      struct {
		Created int64 `json:"created"`
	} `json:"time"`
	Model struct {
		ID string `json:"id"`
	} `json:"model"`
}

type ocPart struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Tool  string          `json:"tool"`
	State json.RawMessage `json:"state"`
}

type ocToolState struct {
	Status string          `json:"status"`
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
	Error  string          `json:"error"`
}

type ocMessage struct {
	Info struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		Time struct {
			Completed int64 `json:"completed"`
		} `json:"time"`
	} `json:"info"`
	Parts []ocPart `json:"parts"`
}

type ocPermission struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionID"`
	Permission string          `json:"permission"`
	Metadata   json.RawMessage `json:"metadata"`
}

func ocGet(base, path string, out any) error {
	resp, err := http.Get(base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func ocReply(base, requestID, reply string) {
	req, err := http.NewRequest(http.MethodPost, base+"/permission/"+requestID+"/reply",
		strings.NewReader(`{"reply":"`+reply+`"}`))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

func ocPartMessages(role string, p ocPart) []Message {
	switch p.Type {
	case "text":
		if p.Text == "" {
			return nil
		}
		return []Message{{Role: role, Kind: "text", Text: truncate(p.Text, maxText)}}
	case "reasoning":
		if p.Text == "" {
			return nil
		}
		return []Message{{Role: role, Kind: "thinking", Text: truncate(p.Text, maxText)}}
	case "tool":
		var st ocToolState
		json.Unmarshal(p.State, &st)
		if st.Status != "completed" && st.Status != "error" {
			return nil
		}
		out := []Message{{Role: role, Kind: "tool_use", Name: p.Tool, Text: truncate(prettyJSON(st.Input), maxText)}}
		if st.Status == "error" {
			out = append(out, Message{Role: role, Kind: "tool_result", Text: truncate(st.Error, maxText)})
		} else if st.Output != "" {
			out = append(out, Message{Role: role, Kind: "tool_result", Text: truncate(st.Output, maxText)})
		}
		return out
	}
	return nil
}

func opencodePoll(ctx context.Context, s *Session, base, cwd string, launchedAt time.Time, knownID string) {
	sessionID := knownID
	recorded := false
	nextLine := 0
	done := map[string]bool{}
	pendingID := ""
	wasBusy := false

	tick := time.NewTicker(600 * time.Millisecond)
	defer tick.Stop()
	for {
		if sessionID == "" {
			var sessions []ocSession
			if ocGet(base, "/session", &sessions) == nil {
				for _, sess := range sessions {
					if sess.Directory == cwd && time.UnixMilli(sess.Time.Created).After(launchedAt) {
						sessionID = sess.ID
						break
					}
				}
			}
		}
		if sessionID != "" && !recorded {
			recorded = true
			s.mu.Lock()
			s.sessionID = sessionID
			s.mu.Unlock()
			go s.d.persist()
		}
		if sessionID != "" {
			var sess ocSession
			if ocGet(base, "/session/"+sessionID, &sess) == nil {
				s.setMeta(sess.Title, "")
			}

			var msgs []ocMessage
			if ocGet(base, "/session/"+sessionID+"/message", &msgs) == nil {
				busy := false
				for _, m := range msgs {
					streaming := m.Info.Role == "assistant" && m.Info.Time.Completed == 0
					if streaming {
						busy = true
					}
					if done[m.Info.ID] || streaming {
						continue
					}
					for _, p := range m.Parts {
						for _, msg := range ocPartMessages(m.Info.Role, p) {
							nextLine++
							msg.Line = nextLine
							s.appendMessage(msg)
						}
					}
					done[m.Info.ID] = true
				}
				if busy != wasBusy {
					wasBusy = busy
					if busy {
						s.setStatus("running")
					} else {
						s.setStatus("idle")
						s.notifyDone()
					}
				}
			}

			var perms []ocPermission
			var mine *ocPermission
			if ocGet(base, "/permission", &perms) == nil {
				for i := range perms {
					if perms[i].SessionID == sessionID {
						mine = &perms[i]
						break
					}
				}
			}
			switch {
			case mine != nil && mine.ID != pendingID:
				pendingID = mine.ID
				s.opencodeAskPermission(mine.ID, mine.Permission, mine.Metadata)
			case mine == nil && pendingID != "":
				s.opencodeClearPermission(pendingID)
				pendingID = ""
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

var opencodeStatusRE = regexp.MustCompile(`(Build|Plan)\s*·\s*(.+)`)

func opencodeStatus(pane string) (mode, rest string) {
	matches := opencodeStatusRE.FindAllStringSubmatch(pane, -1)
	if len(matches) == 0 {
		return "", ""
	}
	last := matches[len(matches)-1]
	return strings.ToLower(last[1]), strings.TrimSpace(last[2])
}

func parseOpencodeMode(pane string) string {
	mode, _ := opencodeStatus(pane)
	return mode
}

func opencodeModelName(pane, base string) string {
	_, rest := opencodeStatus(pane)
	if rest == "" {
		return ""
	}
	cfg := opencodeProviderConfig(base)
	if cfg == nil {
		return rest
	}
	// The TUI status renders "<model> <provider>"; match on that but return the
	// same "<model> (<provider>)" label opencodeModels emits, so the picker can
	// mark the active model.
	best, label := "", ""
	for _, p := range cfg.Providers {
		for _, m := range p.Models {
			full := m.Name + " " + p.Name
			if strings.HasPrefix(rest, full) && len(full) > len(best) {
				best, label = full, m.Name+" ("+p.Name+")"
			}
		}
	}
	if label != "" {
		return label
	}
	return rest
}

func opencodeSetMode(session, target string) {
	if parseOpencodeMode(tmuxCapture(session)) != target {
		tmuxSendKey(session, "Tab")
	}
}

type opencodeProviders struct {
	Providers []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Models map[string]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	} `json:"providers"`
}

func opencodeProviderConfig(base string) *opencodeProviders {
	var cfg opencodeProviders
	if ocGet(base, "/config/providers", &cfg) != nil {
		return nil
	}
	return &cfg
}

func opencodeModels(base string) []map[string]string {
	cfg := opencodeProviderConfig(base)
	if cfg == nil {
		return nil
	}
	var out []map[string]string
	for _, p := range cfg.Providers {
		for _, m := range p.Models {
			out = append(out, map[string]string{"id": p.ID + "/" + m.ID, "label": m.Name + " (" + p.Name + ")"})
		}
	}
	return out
}

func opencodeSetModel(session, base, providerModelID string) {
	providerID, modelID, ok := strings.Cut(providerModelID, "/")
	if !ok {
		return
	}
	cfg := opencodeProviderConfig(base)
	if cfg == nil {
		return
	}
	var name string
	for _, p := range cfg.Providers {
		if p.ID == providerID {
			if m, ok := p.Models[modelID]; ok {
				name = m.Name
			}
		}
	}
	if name == "" {
		return
	}
	tmuxSendKey(session, "C-x")
	tmuxSendKey(session, "m")
	time.Sleep(400 * time.Millisecond)
	if !strings.Contains(tmuxCapture(session), "Select model") {
		return
	}
	tmuxSendText(session, name)
	time.Sleep(400 * time.Millisecond)
	if strings.Contains(tmuxCapture(session), "variant") {
		tmuxSendKey(session, "Enter")
	}
}
