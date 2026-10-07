package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/browser"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func (s *Server) SetBrowser(manager *browser.Manager) { s.browsers = manager }
func (s *Server) handleBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authenticateRequest(w, r, "zen-browser"); !ok {
		return
	}
	if s.browsers == nil {
		http.Error(w, "Browser is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var q browser.Request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&q) != nil {
		http.Error(w, "Invalid browser request", 400)
		return
	}
	// Leases and input belong to authenticated viewer connections, never HTTP bodies.
	if q.Action == "command" || q.Action == "control" || q.Action == "release" {
		http.Error(w, "Use the Browser viewer for control", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.browsers.Handle(ctx, browser.Owner{Kind: "human", ID: "server-owner"}, q)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
func (s *Server) handleBrowserViewer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if len(id) != 36 {
		http.Error(w, "Invalid browser", 400)
		return
	}
	device, ok := s.authenticateRequest(w, r, "zen-browser-view:"+id)
	if !ok {
		return
	}
	if s.browsers == nil {
		http.Error(w, "Browser is unavailable", 503)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	actor := browser.Owner{Kind: "human", ID: device.ID + "/browser/" + uuid.NewString()}
	owner := &authenticatedClient{deviceID: device.ID, browserOwner: &actor}
	if !s.bindAuthenticatedClient(conn, owner) {
		_ = conn.Close()
		return
	}
	defer s.detachAuthenticatedClient(conn, owner)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream, err := s.browsers.OpenStream(ctx, id)
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "error", "error": err.Error()})
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "viewer unavailable"), time.Now().Add(time.Second))
		return
	}
	defer stream.Close()
	var writeMu sync.Mutex
	send := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(v)
	}
	permit := make(chan struct{}, 1)
	permit <- struct{}{}
	var sent atomic.Uint64
	var acked uint64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-permit:
			}
			raw, err := stream.Read()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				cancel()
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1011, "browser stream ended"), time.Now().Add(time.Second))
				_ = conn.Close()
				return
			}
			var frame struct {
				Type     string          `json:"type"`
				Seq      uint64          `json:"seq"`
				Data     string          `json:"data"`
				Metadata json.RawMessage `json:"metadata"`
			}
			if json.Unmarshal(raw, &frame) != nil || frame.Type != "frame" {
				permit <- struct{}{}
				continue
			} // Never forward console or arbitrary browser messages.
			if len(frame.Data) > 2800000 {
				cancel()
				_ = conn.Close()
				return
			}
			sent.Store(frame.Seq)
			if err = send(map[string]any{"type": "frame", "seq": frame.Seq, "data": frame.Data, "metadata": frame.Metadata}); err != nil {
				cancel()
				_ = conn.Close()
				return
			}
		}
	}()
	defer func() { cancel(); _ = stream.Close(); <-done }()
	conn.SetReadLimit(32 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(40 * time.Second))
	for {
		var msg struct {
			Type      string           `json:"type"`
			Seq       uint64           `json:"seq"`
			RequestID string           `json:"request_id"`
			Lease     *browser.Lease   `json:"lease"`
			Command   *browser.Command `json:"command"`
		}
		if err = conn.ReadJSON(&msg); err != nil {
			return
		}
		if owner.revoked.Load() || !s.auth.IsDeviceTrusted(device.ID) {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(40 * time.Second))
		if msg.Type == "ping" {
			state, _ := s.browsers.Handle(ctx, actor, browser.Request{Action: "status", ID: id})
			if err = send(map[string]any{"type": "pong", "resource": state.Resource}); err != nil {
				return
			}
			continue
		}
		if msg.Type == "ack" {
			if msg.Seq <= acked || msg.Seq != sent.Load() {
				continue
			}
			acked = msg.Seq
			if err = stream.Ack(msg.Seq); err != nil {
				return
			}
			select {
			case permit <- struct{}{}:
			default:
			}
			continue
		}
		if msg.Type == "close" {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "viewer detached"), time.Now().Add(time.Second))
			return
		}
		if msg.Type != "control" && msg.Type != "release" && msg.Type != "command" {
			continue
		}
		q := browser.Request{ID: id, Action: msg.Type, Lease: msg.Lease, Command: msg.Command}
		actionCtx, actionCancel := context.WithTimeout(ctx, 12*time.Second)
		result, actionErr := s.browsers.Handle(actionCtx, actor, q)
		actionCancel()
		response := map[string]any{"type": "response", "request_id": msg.RequestID, "response": result}
		if actionErr != nil {
			response["error"] = actionErr.Error()
		}
		if err = send(response); err != nil {
			return
		}
	}
}
