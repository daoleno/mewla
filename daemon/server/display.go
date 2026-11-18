package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/screencast"
	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

var displayUpgrader = websocket.Upgrader{
	ReadBufferSize:  16 << 10,
	WriteBufferSize: 16 << 10,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// Displays are the agents' displays Screen lists, views and stops one by one:
// on Linux each agent's monitor of the agent screen (vdisplay.Service), on a
// Mac the agents' browser windows on the agent display (screen.AgentWindows).
type Displays interface {
	// Capability says why agents have no display here, or nil.
	Capability() error
	List() []vdisplay.Info
	View(worker string) (*vdisplay.Viewer, error)
	Halt(worker string) error
	Resume(worker string) error
}

// SetScreenLoopbackOnly keeps Screen's video on this computer's loopback
// address, for a daemon reachable from this computer only.
func (s *Server) SetScreenLoopbackOnly(on bool) { s.screenLoopbackOnly = on }

// SetDisplays installs the agents' displays.
func (s *Server) SetDisplays(service Displays) { s.displays = service }

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
		// reason says what is missing when agents cannot have a screen (or,
		// on the agent screen, a browser on it) on this computer.
		reason := ""
		capability := s.displays.Capability()
		if capability != nil {
			reason = capability.Error()
		} else if _, agentScreen := s.displays.(*vdisplay.Service); agentScreen && s.browsers != nil {
			if c := s.browsers.Capability(r.Context()); !c.Available {
				reason = c.Reason
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"displays": list, "available": capability == nil, "reason": reason, "video": "webrtc"})
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

// handleDisplayViewer shows one agent's display to an authenticated device as
// pushed video over WebRTC (package screencast); the WebSocket carries the
// signalling and these control messages. What the person does is told to the
// agent (never what they typed). A {"type":"hold","on":true} message holds
// the agent off until released or the socket closes. While held, a
// {"type":"fit","width":w,"height":h,"dpr":d} message lays the agent's browser
// window out for the person's screen (width 0 puts it back); the socket answers
// {"type":"reflow","on":bool,"host":"…"} then and whenever that changes.
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
	if !playsVideo(r) {
		refuseOldApp(conn)
		return
	}
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
	if view.Notify != nil {
		view.Notify(func(state vdisplay.Reflow) {
			sendJSON(map[string]any{"type": "reflow", "on": state.On, "host": state.Host})
		})
	}
	// Fits run one at a time, off the read loop (a window change takes the
	// extension a moment); only the newest waiting one runs.
	fits := make(chan [3]float64, 1)
	if view.Fit != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case f := <-fits:
					state := view.Fit(int(f[0]), int(f[1]), f[2])
					sendJSON(map[string]any{"type": "reflow", "on": state.On, "host": state.Host})
				}
			}
		}()
	}

	if view.Next == nil || view.Input == nil {
		sendJSON(map[string]string{"type": "error", "error": "This display can't be shown"})
		return
	}
	var cast *screencast.Session
	castReady := make(chan struct{})
	conn.SetReadLimit(1 << 20)
	go func() {
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
			if kind != websocket.TextMessage {
				continue // input rides the data channels
			}
			var msg struct {
				Type string          `json:"type"`
				T    json.RawMessage `json:"t"`
				On   bool            `json:"on"`
				// fit
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
				DPR    float64 `json:"dpr"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "rtc", "ice":
				select {
				case <-castReady:
				case <-ctx.Done():
					return
				}
				if cast != nil {
					if err := cast.HandleSignal(data); err != nil {
						log.Printf("display: viewer for %s: signalling: %v", worker, err)
					}
				}
			case "ping":
				sendJSON(map[string]any{"type": "pong", "t": msg.T})
			case "hold":
				view.Hold(msg.On)
			case "fit":
				if view.Fit == nil {
					sendJSON(map[string]any{"type": "reflow", "on": false})
					break
				}
				select {
				case <-fits:
				default:
				}
				fits <- [3]float64{msg.Width, msg.Height, msg.DPR}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		view.Close()
	}()

	// The person's input goes through the display's own relay, which tells
	// the agent what they did.
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	go func() {
		err := view.Input(inputReader)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && ctx.Err() == nil {
			log.Printf("display: viewer for %s: input ended: %v", worker, err)
		}
		_ = inputReader.CloseWithError(err)
	}()
	path := clientaddr.Path(r)
	signOffer, verifyAnswer := s.screenBinding(device.ID)
	cast, err = screencast.Start(ctx, screencast.Config{
		Source: displaySource{view},
		Input: func(msg []byte) error {
			_, err := inputWriter.Write(msg)
			return err
		},
		Signal: func(v any) { sendJSON(v) },
		Stats: func(st screencast.Stats) {
			sendJSON(struct {
				Type string `json:"type"`
				Path string `json:"path"`
				screencast.Stats
			}{"stats", path, st})
		},
		NewEncoder:   screen.CastEncoder(),
		SignOffer:    signOffer,
		VerifyAnswer: verifyAnswer,
		LoopbackOnly: s.screenLoopbackOnly,
		ICEServers:   s.screenICE(ctx),
	})
	close(castReady)
	if err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer cast.Close()
	sendJSON(map[string]string{"type": "ready"})
	select {
	case <-ctx.Done():
	case <-cast.Done():
		if ctx.Err() == nil {
			sendJSON(map[string]string{"type": "error", "error": "The connection to the display was lost"})
		}
	}
}

// screenBinding binds a Screen session's DTLS certificates to this computer
// and to the viewer's paired device: the offer carries this computer's
// signature over its fingerprints, and an answer is used only when the same
// device (the one this socket authenticated) signed the answer's.
func (s *Server) screenBinding(deviceID string) (func([]string) (any, error), func([]string, string) error) {
	sign := func(fingerprints []string) (any, error) {
		if len(fingerprints) == 0 {
			return nil, errors.New("the offer names no DTLS certificate")
		}
		assertion, err := s.auth.CreateServerAssertion(auth.ScreenDTLSPurpose(fingerprints))
		if err != nil {
			return nil, err
		}
		return map[string]string{"timestamp": assertion.Timestamp, "nonce_hex": assertion.NonceHex, "signature_hex": assertion.SignatureHex}, nil
	}
	verify := func(fingerprints []string, authorization string) error {
		if len(fingerprints) == 0 {
			return errors.New("the viewer's answer names no DTLS certificate")
		}
		device, err := s.auth.VerifyAuthorization(authorization, auth.ScreenDTLSPurpose(fingerprints), 5*time.Minute)
		if err != nil {
			return fmt.Errorf("the viewer's answer is not signed by a paired device: %w", err)
		}
		if device.ID != deviceID {
			return errors.New("the viewer's answer is signed by another device")
		}
		return nil
	}
	return sign, verify
}

// screenICE is the relay's TURN (and STUN) for a viewer with no direct path
// (mobile data), when this computer is registered with a relay that has it.
func (s *Server) screenICE(ctx context.Context) []webrtc.ICEServer {
	if s.screenLoopbackOnly {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	creds, err := link.TURN(ctx)
	if err != nil {
		return nil
	}
	return screencast.ICEServersFor(creds.URLs, creds.Username, creds.Password)
}

// displaySource is a display view's pictures for screencast.
type displaySource struct{ view *vdisplay.Viewer }

func (d displaySource) Next(ctx context.Context, after uint64) (screencast.Picture, error) {
	p, err := d.view.Next(ctx, after)
	return screencast.Picture{Pix: p.Pix, Width: p.Width, Height: p.Height, Stride: p.Stride, RGB: p.RGB, Seq: p.Seq}, err
}
