// Package opencodeapi reads OpenCode 2 through its own server API.
//
// OpenCode 2 keeps sessions behind a background service and documents its
// HTTP API as OpenAPI (operation ids such as session.list). Mewla reaches it
// with the official `opencode api <operation>` client, which discovers and
// authenticates to the running service itself, so Mewla never reads the
// service password or OpenCode's private storage tables.
package opencodeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/procspawn"
)

// ErrSessionNotFound means the service does not know the requested session.
var ErrSessionNotFound = errors.New("opencode session not found")

// ErrUnavailable means the opencode CLI is missing or its service is unreachable.
var ErrUnavailable = errors.New("opencode service unavailable")

const callTimeout = 15 * time.Second

// Runner executes `opencode <args...>` and returns stdout. Tests replace it
// with a fake service; production runs the installed CLI. A test binary never
// reaches the developer's real OpenCode service.
var Runner = func(ctx context.Context, args ...string) ([]byte, error) {
	if testing.Testing() {
		return nil, fmt.Errorf("%w: tests must install a fake opencode service", ErrUnavailable)
	}
	// The daemon may run without the user's shell PATH (a login item), so
	// look where OpenCode's installer puts the binary too.
	binary, err := hostdeps.LookPath("opencode")
	if err != nil {
		return nil, fmt.Errorf("%w: opencode not found on PATH", ErrUnavailable)
	}
	// The CLI exits before draining a large response into a pipe (2.0.32 cut
	// session.message.list at 256 KiB with exit 0), so stdout is a file.
	stdout, err := os.CreateTemp("", "mewla-opencode-api-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(stdout.Name())
	defer stdout.Close()
	var stderr bytes.Buffer
	command := procspawn.CommandContext(ctx, "opencode api", binary, args...)
	command.Stdout = stdout
	command.Stderr = &stderr
	runErr := command.Run()
	body, err := os.ReadFile(stdout.Name())
	if err != nil {
		return nil, err
	}
	if runErr != nil {
		// Typed API errors arrive as JSON on stdout with a non-zero exit.
		if len(body) > 0 {
			return body, &callError{status: strings.TrimSpace(stderr.String()), body: body}
		}
		return nil, fmt.Errorf("%w: %v: %s", ErrUnavailable, runErr, strings.TrimSpace(stderr.String()))
	}
	return body, nil
}

type callError struct {
	status string
	body   []byte
}

// TypedError is a failed call whose stdout carried a typed API error body,
// as the CLI reports one. Fake services use it.
func TypedError(body []byte, status error) error {
	return &callError{status: status.Error(), body: body}
}

func (e *callError) Error() string {
	var typed struct {
		Tag     string `json:"_tag"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(e.body, &typed)
	return strings.TrimSpace(fmt.Sprintf("opencode api: %s %s %s", e.status, typed.Tag, typed.Message))
}

// Call runs one OpenAPI operation with path/query parameters and decodes the
// JSON response into result.
func Call(ctx context.Context, operation string, params map[string]string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	args := []string{"api", operation}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--param", key+"="+params[key])
	}
	out, err := Runner(ctx, args...)
	if err != nil {
		var typed *callError
		if errors.As(err, &typed) && bytes.Contains(typed.body, []byte(`"SessionNotFoundError"`)) {
			return ErrSessionNotFound
		}
		return err
	}
	if result == nil {
		return nil
	}
	if len(bytes.TrimSpace(out)) == 0 {
		// A successful exit with no body means the service did not answer.
		return fmt.Errorf("%w: opencode api %s returned no output", ErrUnavailable, operation)
	}
	if err := json.Unmarshal(out, result); err != nil {
		return fmt.Errorf("opencode api %s: %w", operation, err)
	}
	return nil
}

// Session is the subset of Session.Info Mewla consumes.
type Session struct {
	ID       string `json:"id"`
	ParentID string `json:"parentID"`
	Title    string `json:"title"`
	Outcome  string `json:"outcome"`
	Time     struct {
		Created float64 `json:"created"`
		Updated float64 `json:"updated"`
		Idle    float64 `json:"idle"`
	} `json:"time"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
}

// Created returns the session creation time.
func (s Session) Created() time.Time { return millis(s.Time.Created) }

// Updated returns the last session update time.
func (s Session) Updated() time.Time { return millis(s.Time.Updated) }

func millis(value float64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(value))
}

type cursor struct {
	Next *string `json:"next"`
}

// GetSession returns one session by id.
func GetSession(ctx context.Context, id string) (Session, error) {
	var response struct {
		Data Session `json:"data"`
	}
	if err := Call(ctx, "session.get", map[string]string{"sessionID": id}, &response); err != nil {
		return Session{}, err
	}
	return response.Data, nil
}

// SessionQuery filters session.list. Zero fields are omitted.
type SessionQuery struct {
	Directory string
	RootsOnly bool
	// Stop ends paging once a page's sessions were all updated before it;
	// sessions arrive newest-updated first.
	UpdatedSince time.Time
	Limit        int
}

// ListSessions pages session.list newest-updated first.
func ListSessions(ctx context.Context, query SessionQuery) ([]Session, error) {
	params := map[string]string{"limit": "200"}
	if query.Directory != "" {
		params["directory"] = query.Directory
	}
	if query.RootsOnly {
		params["parentID"] = "null"
	}
	var sessions []Session
	for {
		var response struct {
			Data   []Session `json:"data"`
			Cursor cursor    `json:"cursor"`
		}
		if err := Call(ctx, "session.list", params, &response); err != nil {
			return nil, err
		}
		reachedOld := false
		for _, session := range response.Data {
			if !query.UpdatedSince.IsZero() && session.Updated().Before(query.UpdatedSince) {
				reachedOld = true
				continue
			}
			sessions = append(sessions, session)
		}
		if query.Limit > 0 && len(sessions) >= query.Limit {
			return sessions[:query.Limit], nil
		}
		if reachedOld || len(response.Data) < 200 || response.Cursor.Next == nil || *response.Cursor.Next == "" {
			return sessions, nil
		}
		// A cursor carries its own filters; session.list rejects mixing them.
		params = map[string]string{"limit": "200", "cursor": *response.Cursor.Next}
	}
}

// Message is one Session.Message.Info. Raw keeps the full payload so callers
// project the fields they need without a second request.
type Message struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Raw  json.RawMessage
}

// MessagePage is one session.message.list page.
type MessagePage struct {
	Messages []Message
	Next     string
	// Last reports the final page. The service returns a next cursor with
	// every non-empty page, so a page shorter than its limit ends the list.
	Last bool
}

// ListMessagesPage fetches one page. order is "asc" or "desc" on the first
// page; later pages pass the cursor instead.
func ListMessagesPage(ctx context.Context, sessionID, order, pageCursor, messageType string, limit int) (MessagePage, error) {
	params := map[string]string{"sessionID": sessionID, "limit": fmt.Sprint(limit)}
	if pageCursor != "" {
		params["cursor"] = pageCursor
	} else if order != "" {
		params["order"] = order
	}
	if messageType != "" {
		params["type"] = messageType
	}
	var response struct {
		Data   []json.RawMessage `json:"data"`
		Cursor cursor            `json:"cursor"`
	}
	if err := Call(ctx, "session.message.list", params, &response); err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{Messages: make([]Message, 0, len(response.Data))}
	for _, raw := range response.Data {
		var head struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &head) != nil || head.ID == "" {
			continue
		}
		page.Messages = append(page.Messages, Message{ID: head.ID, Type: head.Type, Raw: raw})
	}
	if response.Cursor.Next != nil && len(response.Data) > 0 {
		page.Next = *response.Cursor.Next
	}
	page.Last = page.Next == "" || len(response.Data) < limit
	return page, nil
}

// ListMessages returns every message of a session oldest first.
func ListMessages(ctx context.Context, sessionID, messageType string) ([]Message, error) {
	var messages []Message
	pageCursor := ""
	for {
		page, err := ListMessagesPage(ctx, sessionID, "asc", pageCursor, messageType, 200)
		if err != nil {
			return nil, err
		}
		messages = append(messages, page.Messages...)
		if page.Last {
			return messages, nil
		}
		pageCursor = page.Next
	}
}

// Paths are OpenCode's global directories as `opencode debug paths` reports.
type Paths struct {
	Data   string
	State  string
	Config string
	DB     string
}

// DebugPaths runs `opencode debug paths`.
func DebugPaths(ctx context.Context) (Paths, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := Runner(ctx, "debug", "paths")
	if err != nil {
		return Paths{}, err
	}
	return ParseDebugPaths(out), nil
}

// ParseDebugPaths reads the `name  path` lines of `opencode debug paths`.
func ParseDebugPaths(out []byte) Paths {
	var paths Paths
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value := strings.Join(fields[1:], " ")
		switch fields[0] {
		case "data":
			paths.Data = value
		case "state":
			paths.State = value
		case "config":
			paths.Config = value
		case "db":
			paths.DB = value
		}
	}
	return paths
}

// Version returns the installed CLI version ("2.0.32").
func Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := Runner(ctx, "--version")
	if err != nil {
		return "", err
	}
	return ParseVersion(out), nil
}

// ParseVersion extracts the semantic version from `opencode --version`
// ("opencode v2.0.32" in OpenCode 2, a bare "1.18.4" in OpenCode 1).
func ParseVersion(out []byte) string {
	for _, field := range strings.Fields(string(out)) {
		field = strings.TrimPrefix(field, "v")
		if field != "" && field[0] >= '0' && field[0] <= '9' && strings.Contains(field, ".") {
			return field
		}
	}
	return ""
}

// MajorVersion returns the leading number of a version string, or 0.
func MajorVersion(version string) int {
	major := 0
	for _, r := range version {
		if r < '0' || r > '9' {
			break
		}
		major = major*10 + int(r-'0')
	}
	return major
}

// SupportedMajor is the only OpenCode major version Mewla integrates with.
const SupportedMajor = 2
