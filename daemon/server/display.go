package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/gorilla/websocket"
)

var displayUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 256 << 10,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// SetDisplays installs the private displays agents open apps on.
func (s *Server) SetDisplays(service *vdisplay.Service) { s.displays = service }

// displayOn is whether agents have displays: the Labs switch is on and Xvnc is installed.
func (s *Server) displayOn() bool {
	return s.displays != nil && s.browsers != nil && s.browsers.VirtualScreen() && vdisplay.Capability() == nil
}

// handleDisplay lists the agents' displays (GET) and stops or resumes one (POST).
func (s *Server) handleDisplay(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateRequest(w, r, "mewla-display"); !ok {
		return
	}
	if s.displays == nil {
		http.Error(w, "Displays are unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		list := s.displays.List()
		if list == nil {
			list = []vdisplay.Info{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"displays": list, "available": vdisplay.Capability() == nil})
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var q struct {
			Action string `json:"action"`
			Worker string `json:"worker"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&q) != nil || (q.Action != "halt" && q.Action != "resume") || q.Worker == "" {
			http.Error(w, "Invalid display request", http.StatusBadRequest)
			return
		}
		change := s.displays.Halt
		if q.Action == "resume" {
			change = s.displays.Resume
		}
		if err := change(q.Worker); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleDisplayViewer bridges an authenticated WebSocket to one agent's
// display, speaking RFB to the viewer as the Mac's Screen does. What the
// person does is told to the agent. A {"type":"hold","on":true} message holds
// the agent off until released or the socket closes.
func (s *Server) handleDisplayViewer(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateRequest(w, r, "mewla-display-view")
	if !ok {
		return
	}
	if s.displays == nil {
		http.Error(w, "Displays are unavailable", http.StatusServiceUnavailable)
		return
	}
	worker := r.URL.Query().Get("worker")
	conn, err := displayUpgrader.Upgrade(w, r, websocketAuthResponseHeader(r))
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)

	var writeMu sync.Mutex
	write := func(kind int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(kind, data)
	}
	sendJSON := func(v any) {
		raw, _ := json.Marshal(v)
		_ = write(websocket.TextMessage, raw)
	}
	view, err := s.displays.View(worker)
	if err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer view.Close()

	viewerReader, viewerWriter := io.Pipe()
	defer viewerReader.Close()
	conn.SetReadLimit(1 << 20)
	go func() {
		defer viewerWriter.Close()
		trusted := time.Now()
		for {
			_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
			kind, data, err := conn.ReadMessage()
			if err != nil {
				cancel(err)
				return
			}
			if time.Since(trusted) > 2*time.Second {
				if !s.auth.IsDeviceTrusted(device.ID) {
					cancel(errors.New("device revoked"))
					return
				}
				trusted = time.Now()
			}
			switch kind {
			case websocket.BinaryMessage:
				if _, err := viewerWriter.Write(data); err != nil {
					cancel(err)
					return
				}
			case websocket.TextMessage:
				var msg struct {
					Type string          `json:"type"`
					T    json.RawMessage `json:"t"`
					On   bool            `json:"on"`
				}
				if json.Unmarshal(data, &msg) != nil {
					break
				}
				switch msg.Type {
				case "ping":
					raw, _ := json.Marshal(map[string]any{"type": "pong", "t": msg.T})
					_ = write(websocket.TextMessage, raw)
				case "hold":
					view.Hold(msg.On)
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		view.Close()
		_ = viewerReader.CloseWithError(context.Cause(ctx))
	}()

	if err := vdisplay.AcceptViewer(displayViewerStream{reader: viewerReader, write: write}); err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	sendJSON(map[string]string{"type": "ready"})
	go func() {
		err := view.Relay(viewerReader)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && ctx.Err() == nil {
			log.Printf("display: viewer for %s ended: %v", worker, err)
		}
		cancel(err)
	}()
	buf := make([]byte, 256<<10)
	for {
		n, err := view.Conn.Read(buf)
		if n > 0 {
			if write(websocket.BinaryMessage, append([]byte(nil), buf[:n]...)) != nil {
				return
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				sendJSON(map[string]string{"type": "error", "error": "The display ended"})
			}
			return
		}
	}
}

type displayViewerStream struct {
	reader io.Reader
	write  func(int, []byte) error
}

func (v displayViewerStream) Read(p []byte) (int, error) { return v.reader.Read(p) }
func (v displayViewerStream) Write(p []byte) (int, error) {
	if err := v.write(websocket.BinaryMessage, append([]byte(nil), p...)); err != nil {
		return 0, err
	}
	return len(p), nil
}
