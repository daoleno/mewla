package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/gorilla/websocket"
)

// RFB payloads are already compressed or are pixels the phone would have to
// inflate on its JS thread, so the screen socket never negotiates deflate.
var screenUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 64 << 10,
}

// screenControllers admits one viewer at a time; a newer viewer replaces the
// older one, which is told why before it closes.
type screenControllers struct {
	mu      sync.Mutex
	current *screenController
}

type screenController struct {
	cancel context.CancelCauseFunc
}

var errScreenReplaced = errors.New("Another device took over this screen")

func (c *screenControllers) take(cancel context.CancelCauseFunc) *screenController {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil {
		c.current.cancel(errScreenReplaced)
	}
	c.current = &screenController{cancel: cancel}
	return c.current
}

func (c *screenControllers) release(owner *screenController) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == owner {
		c.current = nil
	}
}

func (s *Server) SetScreen(service *screen.Service) {
	s.screen = service
	// Devices revoked while Mewla was stopped.
	s.forgetUnpairedScreenDevices()
}

// forgetUnpairedScreenDevices drops Screen approvals of devices that are no
// longer paired.
func (s *Server) forgetUnpairedScreenDevices() {
	if s.screen == nil || s.auth == nil {
		return
	}
	if err := s.screen.Forget(s.auth.IsDeviceTrusted); err != nil {
		log.Printf("screen: forget revoked devices: %v", err)
	}
}

// handleScreen reports this computer's Screen support, permissions and the
// asking device's approval (GET), or runs one action (POST):
//
//	{"action":"enable"}                              ask the person at the computer to allow this device
//	{"action":"open_settings","permission":"<id>"}   show a permission's settings page on the computer
//	{"keep_awake":true|false}                        change keep-awake
//	{"action":"restart"}                             restart Mewla to apply a permission (202)
//
// Enable waits for the answer on the computer, up to a few minutes.
func (s *Server) handleScreen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device, ok := s.authenticateRequest(w, r, "mewla-screen")
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if s.screen == nil {
		_ = json.NewEncoder(w).Encode(screen.Status{Reason: screen.ErrUnsupported.Error()})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Action     string `json:"action"`
			Permission string `json:"permission"`
			KeepAwake  *bool  `json:"keep_awake"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&body) != nil {
			http.Error(w, "Invalid screen request", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		var err error
		switch {
		case body.Action == "enable":
			name := device.Name
			if name == "" {
				name = "A paired device"
			}
			err = s.screen.Enable(ctx, device.ID, name)
		case body.Action == "open_settings" && body.Permission != "":
			err = s.screen.OpenSettings(ctx, device.ID, body.Permission)
		case body.Action == "" && body.KeepAwake != nil:
			err = s.screen.SetKeepAwake(device.ID, *body.KeepAwake)
		case body.Action == "restart":
			if err = s.screen.Restart(device.ID); err == nil {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]bool{"restarting": true})
				return
			}
		default:
			http.Error(w, "Invalid screen request", http.StatusBadRequest)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(s.screen.Status(r.Context(), device.ID))
}

// handleScreenViewer bridges an authenticated WebSocket to an in-process RFB
// session of this computer's screen. Binary messages carry RFB bytes; text
// messages carry JSON control (ping/pong, error). The viewer is offered
// security type None: its device signature and approval already admit it.
func (s *Server) handleScreenViewer(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateRequest(w, r, "mewla-screen-view")
	if !ok {
		return
	}
	if s.screen == nil {
		http.Error(w, "Screen control is unavailable", http.StatusServiceUnavailable)
		return
	}
	conn, err := screenUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)

	outbox := make(chan screenMessage, 32)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				// Flush what is queued, such as an error that ended the session.
				for drained := false; !drained; {
					select {
					case msg := <-outbox:
						_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
						if conn.WriteMessage(msg.kind, msg.data) != nil {
							drained = true
						}
					default:
						drained = true
					}
				}
				reason := "screen closed"
				if cause := context.Cause(ctx); errors.Is(cause, errScreenReplaced) {
					reason = cause.Error()
				}
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), time.Now().Add(time.Second))
				return
			case msg := <-outbox:
				_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
				if err := conn.WriteMessage(msg.kind, msg.data); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	defer func() { cancel(nil); <-writerDone }()
	send := func(kind int, data []byte) bool {
		select {
		case outbox <- screenMessage{kind: kind, data: data}:
			return true
		case <-ctx.Done():
			return false
		}
	}
	sendJSON := func(v any) bool {
		raw, _ := json.Marshal(v)
		return send(websocket.TextMessage, raw)
	}

	upstream, err := s.screen.Open(ctx, device.ID)
	if err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer upstream.Close()
	controller := s.screenControllers.take(cancel)
	defer s.screenControllers.release(controller)
	name := device.Name
	if name == "" {
		name = "A paired device"
	}
	screen.WakeDisplay()
	screen.Notify("Mewla Screen", name+" is viewing and controlling this computer.")
	started := time.Now()
	var fromScreen, toScreen atomic.Int64
	defer func() {
		screen.Notify("Mewla Screen", name+" stopped controlling this computer.")
		log.Printf("screen: device %s session ended after %s, %d bytes to viewer, %d bytes from viewer",
			device.ID, time.Since(started).Round(time.Second), fromScreen.Load(), toScreen.Load())
	}()

	// Viewer bytes flow through a pipe so the handshake and the copy below
	// read one ordered stream regardless of WebSocket message boundaries.
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
			// Revoking the device or turning Screen off ends control within seconds.
			if time.Since(trusted) > 2*time.Second {
				if !s.auth.IsDeviceTrusted(device.ID) {
					cancel(errors.New("device revoked"))
					return
				}
				if !s.screen.Allowed(device.ID) {
					sendJSON(map[string]string{"type": "error", "error": screen.ErrNotApproved.Error()})
					cancel(screen.ErrNotApproved)
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
				}
				if json.Unmarshal(data, &msg) == nil && msg.Type == "ping" {
					sendJSON(map[string]any{"type": "pong", "t": msg.T})
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = upstream.Close()
		_ = viewerReader.CloseWithError(context.Cause(ctx))
	}()

	handshake := screenViewerStream{reader: viewerReader, send: send}
	if err := screen.AcceptClient(handshake); err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	sendJSON(map[string]string{"type": "ready"})
	go func() {
		n, err := io.Copy(upstream, viewerReader)
		toScreen.Add(n)
		cancel(err)
	}()
	buf := make([]byte, 256<<10)
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			fromScreen.Add(int64(n))
			chunk := append([]byte(nil), buf[:n]...)
			if !send(websocket.BinaryMessage, chunk) {
				return
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				sendJSON(map[string]string{"type": "error", "error": "The screen session ended"})
			}
			return
		}
	}
}

type screenMessage struct {
	kind int
	data []byte
}

type screenViewerStream struct {
	reader io.Reader
	send   func(int, []byte) bool
}

func (v screenViewerStream) Read(p []byte) (int, error) { return v.reader.Read(p) }

func (v screenViewerStream) Write(p []byte) (int, error) {
	if !v.send(websocket.BinaryMessage, append([]byte(nil), p...)) {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
