// Package opencodeapitest is an in-memory OpenCode 2 service for tests. It
// answers the `opencode api` operations Mewla uses with the shapes the real
// service returns.
package opencodeapitest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/opencodeapi"
)

// Session seeds one Session.Info.
type Session struct {
	ID        string
	ParentID  string
	Directory string
	Created   time.Time
	Updated   time.Time
}

// Service is a fake OpenCode service. Messages are raw Session.Message.Info
// JSON objects kept oldest first.
type Service struct {
	mu       sync.Mutex
	sessions map[string]Session
	messages map[string][]json.RawMessage
	// DBPath is the file `opencode debug paths` reports; Touch changes its
	// stamp the way an OpenCode write does.
	DBPath string
	// Calls counts `opencode api` operations by id.
	Calls map[string]int
	// Unavailable makes every call fail as if the CLI were missing.
	Unavailable bool
}

// Install replaces opencodeapi.Runner for the test's lifetime.
func Install(t testing.TB) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	if err := os.WriteFile(dbPath, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		sessions: map[string]Session{},
		messages: map[string][]json.RawMessage{},
		DBPath:   dbPath,
		Calls:    map[string]int{},
	}
	previous := opencodeapi.Runner
	opencodeapi.Runner = service.run
	t.Setenv("MEWLA_OPENCODE_DB", dbPath)
	t.Cleanup(func() { opencodeapi.Runner = previous })
	return service
}

// PutSession creates or replaces a session.
func (s *Service) PutSession(session Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	s.touchLocked()
}

// DeleteSession removes a session and its messages.
func (s *Service) DeleteSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	delete(s.messages, id)
	s.touchLocked()
}

// PutMessage appends a message or replaces the one with the same id. message
// is any JSON-marshalable Session.Message.Info value.
func (s *Service) PutMessage(sessionID string, message any) {
	raw, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	var head struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &head)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[sessionID]
	for index, existing := range list {
		var other struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(existing, &other)
		if other.ID == head.ID {
			list[index] = raw
			s.touchLocked()
			return
		}
	}
	s.messages[sessionID] = append(list, raw)
	s.touchLocked()
}

// RemoveMessage deletes one message.
func (s *Service) RemoveMessage(sessionID, messageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.messages[sessionID]
	for index, existing := range list {
		var other struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(existing, &other)
		if other.ID == messageID {
			s.messages[sessionID] = append(list[:index:index], list[index+1:]...)
			break
		}
	}
	s.touchLocked()
}

// Touch changes the database stamp without changing content.
func (s *Service) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touchLocked()
}

func (s *Service) touchLocked() {
	info, err := os.Stat(s.DBPath)
	size := int64(1)
	if err == nil {
		size = info.Size() + 1
	}
	_ = os.WriteFile(s.DBPath, []byte(strings.Repeat("x", int(size))), 0o600)
	stamp := time.Now().Add(time.Duration(size) * time.Millisecond)
	_ = os.Chtimes(s.DBPath, stamp, stamp)
}

// CallCount returns how often an operation ran.
func (s *Service) CallCount(operation string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Calls[operation]
}

func (s *Service) run(_ context.Context, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Unavailable {
		return nil, fmt.Errorf("%w: fake", opencodeapi.ErrUnavailable)
	}
	if len(args) >= 2 && args[0] == "debug" && args[1] == "paths" {
		return []byte("data       /fake/data\ndb         " + s.DBPath + "\n"), nil
	}
	if len(args) == 1 && args[0] == "--version" {
		return []byte("opencode v2.0.32\n"), nil
	}
	if len(args) < 2 || args[0] != "api" {
		return nil, fmt.Errorf("fake opencode: unsupported %v", args)
	}
	operation := args[1]
	params := map[string]string{}
	for index := 2; index+1 < len(args); index += 2 {
		if args[index] != "--param" {
			return nil, fmt.Errorf("fake opencode: unsupported flag %q", args[index])
		}
		key, value, _ := strings.Cut(args[index+1], "=")
		params[key] = value
	}
	s.Calls[operation]++
	switch operation {
	case "session.get":
		session, ok := s.sessions[params["sessionID"]]
		if !ok {
			return notFound(params["sessionID"])
		}
		return json.Marshal(map[string]any{"data": sessionInfo(session)})
	case "session.list":
		return s.listSessions(params)
	case "session.message.list":
		return s.listMessages(params)
	}
	return nil, fmt.Errorf("fake opencode: unsupported operation %s", operation)
}

type notFoundError struct{}

func (notFoundError) Error() string { return "HTTP 404 Not Found" }

func notFound(id string) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{"_tag": "SessionNotFoundError", "sessionID": id, "message": "Session not found: " + id})
	// The real CLI prints the typed error on stdout and exits non-zero.
	return nil, opencodeapi.TypedError(body, notFoundError{})
}

func sessionInfo(session Session) map[string]any {
	info := map[string]any{
		"id":        session.ID,
		"projectID": "global",
		"cost":      0,
		"tokens":    map[string]any{"input": 0, "output": 0, "reasoning": 0, "cache": map[string]int{"read": 0, "write": 0}},
		"time":      map[string]int64{"created": session.Created.UnixMilli(), "updated": session.Updated.UnixMilli()},
		"location":  map[string]string{"directory": session.Directory},
	}
	if session.ParentID != "" {
		info["parentID"] = session.ParentID
	}
	return info
}

type pageCursor struct {
	Offset    int               `json:"offset"`
	Order     string            `json:"order"`
	Filters   map[string]string `json:"filters"`
	Operation string            `json:"operation"`
}

func encodeCursor(cursor pageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(value string) (pageCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return pageCursor{}, err
	}
	var cursor pageCursor
	return cursor, json.Unmarshal(raw, &cursor)
}

func pageLimit(params map[string]string, fallback int) int {
	if limit, err := strconv.Atoi(params["limit"]); err == nil && limit > 0 {
		return limit
	}
	return fallback
}

func (s *Service) listSessions(params map[string]string) ([]byte, error) {
	filters := map[string]string{"directory": params["directory"], "parentID": params["parentID"]}
	offset := 0
	if value := params["cursor"]; value != "" {
		cursor, err := decodeCursor(value)
		if err != nil {
			return nil, err
		}
		filters, offset = cursor.Filters, cursor.Offset
	}
	var sessions []Session
	for _, session := range s.sessions {
		if filters["directory"] != "" && session.Directory != filters["directory"] {
			continue
		}
		if filters["parentID"] == "null" && session.ParentID != "" {
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].Updated.Equal(sessions[j].Updated) {
			return sessions[i].Updated.After(sessions[j].Updated)
		}
		return sessions[i].ID < sessions[j].ID
	})
	limit := pageLimit(params, 50)
	end := min(offset+limit, len(sessions))
	if offset > len(sessions) {
		offset = end
	}
	data := []map[string]any{}
	for _, session := range sessions[offset:end] {
		data = append(data, sessionInfo(session))
	}
	var next any
	if end < len(sessions) || len(data) > 0 {
		next = encodeCursor(pageCursor{Offset: end, Filters: filters})
	}
	return json.Marshal(map[string]any{"data": data, "cursor": map[string]any{"previous": nil, "next": next}})
}

func (s *Service) listMessages(params map[string]string) ([]byte, error) {
	sessionID := params["sessionID"]
	if _, ok := s.sessions[sessionID]; !ok {
		return notFound(sessionID)
	}
	order := params["order"]
	messageType := params["type"]
	offset := 0
	if value := params["cursor"]; value != "" {
		cursor, err := decodeCursor(value)
		if err != nil {
			return nil, err
		}
		order, offset, messageType = cursor.Order, cursor.Offset, cursor.Filters["type"]
	}
	if order == "" {
		order = "asc"
	}
	var all []json.RawMessage
	for _, raw := range s.messages[sessionID] {
		if messageType != "" {
			var head struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(raw, &head)
			if head.Type != messageType {
				continue
			}
		}
		all = append(all, raw)
	}
	if order == "desc" {
		reversed := make([]json.RawMessage, len(all))
		for index, raw := range all {
			reversed[len(all)-1-index] = raw
		}
		all = reversed
	}
	limit := pageLimit(params, 50)
	if offset > len(all) {
		offset = len(all)
	}
	end := min(offset+limit, len(all))
	data := all[offset:end]
	if data == nil {
		data = []json.RawMessage{}
	}
	// Like the real service, a page with data always carries a next cursor.
	var next any
	if end < len(all) || len(data) > 0 {
		next = encodeCursor(pageCursor{Offset: end, Order: order, Filters: map[string]string{"type": messageType}})
	}
	return json.Marshal(map[string]any{"data": data, "cursor": map[string]any{"previous": nil, "next": next}})
}

// User is a user Session.Message.Info.
func User(id string, created time.Time, text string) map[string]any {
	return map[string]any{"id": id, "type": "user", "time": map[string]int64{"created": created.UnixMilli()}, "text": text, "files": []any{}, "agents": []any{}}
}

// Assistant is an assistant Session.Message.Info. A zero completed leaves it
// streaming.
func Assistant(id string, created, completed time.Time, content ...map[string]any) map[string]any {
	timing := map[string]int64{"created": created.UnixMilli()}
	if !completed.IsZero() {
		timing["completed"] = completed.UnixMilli()
	}
	if content == nil {
		content = []map[string]any{}
	}
	message := map[string]any{
		"id": id, "type": "assistant", "agent": "build", "time": timing, "content": content,
		"model":  map[string]string{"id": "deepseek-v4.1-flash", "providerID": "opencode-go"},
		"tokens": map[string]any{"input": 100, "output": 20, "reasoning": 5, "cache": map[string]int{"read": 50, "write": 0}},
		"cost":   0.001,
	}
	if !completed.IsZero() {
		message["finish"] = "stop"
	}
	return message
}

// Idle closes a turn with outcome succeeded, failed or interrupted.
func Idle(id string, created time.Time, outcome string) map[string]any {
	return map[string]any{"id": id, "type": "idle", "time": map[string]int64{"created": created.UnixMilli()}, "outcome": outcome}
}

// Text is assistant text content.
func Text(text string) map[string]any { return map[string]any{"type": "text", "text": text} }

// Reasoning is assistant reasoning content.
func Reasoning(text string) map[string]any { return map[string]any{"type": "reasoning", "text": text} }

// Tool is assistant tool content. status is streaming, running, completed or
// error; output fills completed content or the error message.
func Tool(id, name, status string, input map[string]any, output string) map[string]any {
	state := map[string]any{"status": status}
	switch status {
	case "streaming":
		raw, _ := json.Marshal(input)
		state["input"] = string(raw)
	case "running":
		state["input"], state["metadata"] = input, map[string]any{}
	case "completed":
		state["input"], state["content"] = input, []map[string]string{{"type": "text", "text": output}}
	case "error":
		state["input"], state["error"] = input, map[string]string{"type": "unknown", "message": output}
	}
	return map[string]any{"type": "tool", "id": id, "name": name, "state": state, "time": map[string]int64{"created": 0}}
}
