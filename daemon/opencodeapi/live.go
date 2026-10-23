package opencodeapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Live is a connection to the OpenCode service for what `opencode api` cannot
// do: hold the event stream open. The CLI buffers a streaming response and
// prints nothing, so Live talks to the service itself. It never reads the
// service password; the official client is asked for a single-use pairing
// code (`server.pair`), which the service exchanges for a session token.
type Live struct {
	base   string
	token  string
	client *http.Client
	// stream carries the event stream, which must outlive any request timeout.
	stream *http.Client
}

// StatusError is a refusal from the service.
type StatusError struct {
	Code int
	Tag  string
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("opencode service: HTTP %d %s", e.Code, strings.TrimSpace(e.Tag+" "+e.Body))
}

// Rejected reports that the service refused the request itself: it was
// understood and not carried out, so a send was not accepted.
func Rejected(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Code >= 400 && status.Code < 500 && status.Code != http.StatusUnauthorized
}

// OpenLive connects to the background service the `opencode` command uses.
func OpenLive(ctx context.Context) (*Live, error) {
	var info struct {
		URLs []string `json:"urls"`
	}
	if err := Call(ctx, "server.info", nil, &info); err != nil {
		return nil, err
	}
	base := ""
	for _, candidate := range info.URLs {
		if parsed, err := url.Parse(candidate); err == nil && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1") {
			base = strings.TrimRight(candidate, "/")
			break
		}
	}
	if base == "" {
		return nil, fmt.Errorf("%w: the service reports no loopback address", ErrUnavailable)
	}
	l := &Live{base: base, client: &http.Client{Timeout: 20 * time.Second}, stream: &http.Client{}}
	if err := l.pair(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

// pair exchanges a pairing code from the official client for a session token.
func (l *Live) pair(ctx context.Context) error {
	var code struct {
		Code string `json:"code"`
	}
	if err := Call(ctx, "server.pair", nil, &code); err != nil {
		return err
	}
	if code.Code == "" {
		return fmt.Errorf("%w: server.pair returned no code", ErrUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base+"/auth/connect/"+url.PathEscape(code.Code), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	body, err := l.do(req, false)
	if err != nil {
		return err
	}
	var session struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(body, &session) != nil || session.Token == "" {
		return fmt.Errorf("%w: the service gave no session token", ErrUnavailable)
	}
	l.token = session.Token
	return nil
}

func (l *Live) do(req *http.Request, authed bool) ([]byte, error) {
	if authed {
		req.SetBasicAuth("opencode", l.token)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var typed struct {
			Tag     string `json:"_tag"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &typed)
		return nil, &StatusError{Code: resp.StatusCode, Tag: typed.Tag, Body: typed.Message}
	}
	return body, nil
}

// call makes an authenticated request, pairing again once if the token was
// refused (the service restarted or the token expired).
func (l *Live) call(ctx context.Context, method, path string, body any) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, l.base+path, reader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		out, err := l.do(req, true)
		var status *StatusError
		if errors.As(err, &status) && status.Code == http.StatusUnauthorized && attempt == 0 {
			if perr := l.pair(ctx); perr != nil {
				return nil, perr
			}
			continue
		}
		return out, err
	}
}

// Prompt submits a user message to the session. id is the client's own
// message id (it must start with "msg_") and comes back as the receipt.
func (l *Live) Prompt(ctx context.Context, sessionID, id, text string) (string, error) {
	out, err := l.call(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/prompt", map[string]any{"id": id, "text": text})
	if err != nil {
		return "", err
	}
	var receipt struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &receipt) != nil || receipt.Data.ID == "" {
		return "", fmt.Errorf("opencode service: prompt returned no message id")
	}
	return receipt.Data.ID, nil
}

// Interrupt stops the session's running execution.
func (l *Live) Interrupt(ctx context.Context, sessionID string) error {
	_, err := l.call(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/interrupt", nil)
	return err
}

// Reply answers a pending permission request: once, always or reject.
func (l *Live) Reply(ctx context.Context, sessionID, requestID, decision string) error {
	_, err := l.call(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/permission/"+url.PathEscape(requestID)+"/reply", map[string]any{"decision": decision})
	return err
}

// SessionExists reports whether the service knows the session. A session
// launched with --session exists once its first prompt has been sent.
func (l *Live) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	_, err := l.call(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID), nil)
	var status *StatusError
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

// Active lists the sessions with a running execution.
func (l *Live) Active(ctx context.Context) (map[string]bool, error) {
	out, err := l.call(ctx, http.MethodGet, "/api/session/active", nil)
	if err != nil {
		return nil, err
	}
	var active struct {
		Data map[string]struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &active); err != nil {
		return nil, fmt.Errorf("opencode service: active sessions: %w", err)
	}
	ids := make(map[string]bool, len(active.Data))
	for id, state := range active.Data {
		ids[id] = state.Type == "running"
	}
	return ids, nil
}

// Permission is a permission request the service is waiting on.
type Permission struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
}

// Permissions lists the session's pending permission requests.
func (l *Live) Permissions(ctx context.Context, sessionID string) ([]Permission, error) {
	out, err := l.call(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/permission", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Data []Permission `json:"data"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("opencode service: permissions: %w", err)
	}
	return list.Data, nil
}

// Outcome is how the session's latest execution ended.
type Outcome struct {
	// Status is "succeeded", "failed" or "interrupted"; empty when the session
	// has no ended execution.
	Status    string
	ErrorType string
	ErrorText string
}

// LastOutcome reads the newest "idle" entry of the session, which records how
// the execution that just ended finished.
func (l *Live) LastOutcome(ctx context.Context, sessionID string) (Outcome, error) {
	out, err := l.call(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/message?limit=10", nil)
	if err != nil {
		return Outcome{}, err
	}
	var list struct {
		Data []struct {
			Type    string `json:"type"`
			Outcome string `json:"outcome"`
			Error   struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return Outcome{}, fmt.Errorf("opencode service: messages: %w", err)
	}
	for _, entry := range list.Data { // newest first
		if entry.Type == "idle" {
			return Outcome{Status: entry.Outcome, ErrorType: entry.Error.Type, ErrorText: entry.Error.Message}, nil
		}
	}
	return Outcome{}, nil
}

// StreamIdle is how long the event stream may stay silent, heartbeats
// included, before it is taken as dead.
const StreamIdle = 45 * time.Second

// Stream calls handle with the JSON of every event until the stream ends or
// ctx is done. A stream that falls silent for StreamIdle is closed and
// reported as an error, so the caller reconnects.
func (l *Live) Stream(ctx context.Context, handle func(raw []byte)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base+"/api/event", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", l.token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := l.stream.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized {
			if perr := l.pair(ctx); perr != nil {
				return perr
			}
		}
		return &StatusError{Code: resp.StatusCode}
	}
	idle := time.AfterFunc(StreamIdle, cancel)
	defer idle.Stop()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	var data []byte
	for scanner.Scan() {
		idle.Reset(StreamIdle)
		line := scanner.Bytes()
		switch {
		case len(line) == 0:
			if len(data) > 0 {
				handle(data)
				data = nil
			}
		case bytes.HasPrefix(line, []byte("data:")):
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" "))...)
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("opencode event stream: %w", err)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("opencode event stream: %w", err)
	}
	return io.EOF
}
