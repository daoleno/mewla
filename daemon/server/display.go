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

	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/screencast"
	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

var displayUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 256 << 10,
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
		_ = json.NewEncoder(w).Encode(map[string]any{"displays": list, "available": capability == nil, "reason": reason})
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

	// video=webrtc: the picture is pushed video over WebRTC (package
	// screencast) and this socket carries only its signalling beside the
	// control messages; otherwise it carries RFB.
	var cast *screencast.Session
	castReady := make(chan struct{})
	if r.URL.Query().Get("video") != "webrtc" || view.Next == nil || view.Input == nil {
		close(castReady)
	}

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
					// fit
					Width  float64 `json:"width"`
					Height float64 `json:"height"`
					DPR    float64 `json:"dpr"`
				}
				if json.Unmarshal(data, &msg) != nil {
					break
				}
				switch msg.Type {
				case "rtc", "ice":
					<-castReady
					if cast != nil {
						if err := cast.HandleSignal(data); err != nil {
							log.Printf("display: viewer for %s: signalling: %v", worker, err)
						}
					}
				case "ping":
					raw, _ := json.Marshal(map[string]any{"type": "pong", "t": msg.T})
					_ = write(websocket.TextMessage, raw)
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
		}
	}()
	go func() {
		<-ctx.Done()
		view.Close()
		_ = viewerReader.CloseWithError(context.Cause(ctx))
	}()

	if r.URL.Query().Get("video") == "webrtc" && view.Next != nil && view.Input != nil {
		s.castDisplay(ctx, cancel, worker, r, view, sendJSON, &cast, castReady)
		return
	}
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

// castDisplay streams an agent's display as video until the viewer leaves.
// The person's input goes through the display's own relay, which tells the
// agent what they did (never what they typed), as the RFB path does.
func (s *Server) castDisplay(ctx context.Context, cancel context.CancelCauseFunc, worker string, r *http.Request, view *vdisplay.Viewer, sendJSON func(any), cast **screencast.Session, castReady chan struct{}) {
	// The relay reads a viewer's RFB stream: ClientInit, then messages. The
	// display's own RFB view answers ClientInit with its ServerInit; nothing
	// reads it here.
	go func() { _, _ = io.Copy(io.Discard, view.Conn) }()
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	go func() {
		err := view.Input(inputReader)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && ctx.Err() == nil {
			log.Printf("display: viewer for %s: input ended: %v", worker, err)
		}
		_ = inputReader.CloseWithError(err)
	}()
	if _, err := inputWriter.Write([]byte{1}); err != nil {
		close(castReady)
		return
	}
	path := clientaddr.Path(r)
	source := displaySource{view}
	session, err := screencast.Start(ctx, screencast.Config{
		Source: source,
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
		LoopbackOnly: s.screenLoopbackOnly,
		ICEServers:   s.screenICE(ctx),
	})
	*cast = session
	close(castReady)
	if err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer session.Close()
	sendJSON(map[string]string{"type": "ready"})
	select {
	case <-ctx.Done():
	case <-session.Done():
		if ctx.Err() == nil {
			sendJSON(map[string]string{"type": "error", "error": "The connection to the display was lost"})
			cancel(session.Err())
		}
	}
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
