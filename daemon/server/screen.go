package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/screencast"
	"github.com/gorilla/websocket"
)

// castEncoder makes the views' encoder (nil: ffmpeg); tests replace it.
var castEncoder = screen.CastEncoder

// The screen socket carries only signalling and small control messages.
var screenUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  16 << 10,
	WriteBufferSize: 16 << 10,
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
			Worker     string `json:"worker"`
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
		case body.Action == "stop_agent":
			// Stopping an agent is always safe, so any paired device may. With a
			// Worker, only that one's control ends.
			if body.Worker != "" {
				s.screen.StopAgentFor(body.Worker)
			} else {
				s.screen.StopAgentsByPerson()
			}
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
	status := s.screen.Status(r.Context(), device.ID)
	status.Local = clientaddr.Local(r)
	status.Video = "webrtc"
	if !playsVideo(r) && status.Supported {
		// It shows "Screen can't start on this Mac" with this reason.
		status.Supported, status.Ready, status.Reason = false, false, oldAppScreen
	}
	_ = json.NewEncoder(w).Encode(status)
}

// An app that plays pushed video says so with video=webrtc on its Screen
// requests and viewer sockets. One without it (a release from before the
// video) knows only the RFB stream this computer no longer serves: it is told, in
// words it already shows, to update.
const oldAppScreen = "Update the Mewla app to see this screen"

func playsVideo(r *http.Request) bool { return r.URL.Query().Get("video") == "webrtc" }

// refuseOldApp ends a viewer socket of an app that cannot play the video,
// with the error and close reason that app shows.
func refuseOldApp(conn *websocket.Conn) {
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = conn.WriteJSON(map[string]string{"type": "error", "error": oldAppScreen})
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, oldAppScreen), time.Now().Add(time.Second))
}

// handleScreenViewer shows this computer's screen (or an agent's use of it)
// to an authenticated, allowed device as pushed video over WebRTC. The
// WebSocket carries JSON: the offer, answer and candidates, ping/pong, the
// lock, stats and errors.
func (s *Server) handleScreenViewer(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateRequest(w, r, "mewla-screen-view")
	if !ok {
		return
	}
	if s.screen == nil {
		http.Error(w, "Screen control is unavailable", http.StatusServiceUnavailable)
		return
	}
	conn, err := screenUpgrader.Upgrade(w, r, websocketAuthResponseHeader(r))
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

	out := startViewerOutbox(ctx, cancel, conn, func(cause error) string {
		if errors.Is(cause, screen.ErrScreenReplaced) {
			return cause.Error()
		}
		return "screen closed"
	})
	defer out.wait()
	sendJSON, advise := out.sendJSON, out.advise

	// watch=1 watches the agent using the screen without taking over: the
	// viewer sees the agent's capture; what the person does reaches the screen
	// and is told to the agent.
	watch := r.URL.Query().Get("watch") == "1"
	path := clientaddr.Path(r)
	// Advisories may be dropped, so the lock rides on every stats message too.
	var locked atomic.Bool
	observer := &screen.Observer{
		Locked: func(now bool) {
			locked.Store(now)
			advise(map[string]any{"type": "state", "locked": now})
		},
	}
	var cast *screen.Cast
	if watch {
		cast, err = s.screen.WatchWorkerCast(ctx, device.ID, r.URL.Query().Get("worker"))
	} else {
		cast, err = s.screen.OpenCast(ctx, device.ID, observer)
	}
	if err != nil {
		sendJSON(map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer cast.Close()
	name := device.Name
	if name == "" {
		name = "A paired device"
	}
	doing, ended := "viewing and controlling", "stopped controlling"
	if watch {
		doing, ended = "watching the agent on", "stopped watching"
	} else {
		// A person comes first: this ends an agent's control of the screen.
		controller := s.screen.TakeControl(cancel)
		defer s.screen.ReleaseControl(controller)
		screen.WakeDisplay()
	}
	screen.Notify("Mewla Screen", name+" is "+doing+" this computer.")
	started := time.Now()
	// What the session sent, for one log line at its end. Numbers only.
	var total struct {
		sync.Mutex
		updates, keys, bytes int
		encoder, route       string
	}
	defer func() {
		screen.Notify("Mewla Screen", name+" "+ended+" this computer.")
		total.Lock()
		defer total.Unlock()
		log.Printf("screen: device %s session ended after %s via %s: %d updates (%d key frames), %d KB, encoder %s, route %s",
			device.ID, time.Since(started).Round(time.Second), path, total.updates, total.keys, total.bytes>>10, total.encoder, total.route)
	}()

	// The picture is pushed video over WebRTC (package screencast); this
	// socket carries its signalling beside the control messages.
	var session *screencast.Session
	castReady := make(chan struct{})
	conn.SetReadLimit(1 << 20)
	// Revoking the device or turning Screen off ends the view within seconds,
	// whatever the viewer sends (its input rides the data channels).
	go s.keepTrusted(ctx, cancel, device.ID, func() error {
		if !s.screen.Allowed(device.ID) {
			sendJSON(map[string]string{"type": "error", "error": screen.ErrNotApproved.Error()})
			return screen.ErrNotApproved
		}
		return nil
	})
	go func() {
		for {
			_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
			kind, data, err := conn.ReadMessage()
			if err != nil {
				cancel(err)
				return
			}
			if kind != websocket.TextMessage {
				continue // input rides the data channels
			}
			var msg struct {
				Type string          `json:"type"`
				T    json.RawMessage `json:"t"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "ping":
				sendJSON(map[string]any{"type": "pong", "t": msg.T})
			case "rtc", "ice":
				select {
				case <-castReady:
				case <-ctx.Done():
					return
				}
				if session != nil {
					if err := session.HandleSignal(data); err != nil {
						log.Printf("screen: device %s signalling: %v", device.ID, err)
					}
				}
			}
		}
	}()

	signOffer, verifyAnswer := s.screenBinding(device.ID)
	iceServers, viewerICE, relayMaxKbps := s.screenICE(ctx)
	session, err = screencast.Start(ctx, screencast.Config{
		Source:     cast,
		Input:      cast.Input,
		NewEncoder: castEncoder(),
		Signal:     func(v any) { sendJSON(v) },
		Stats: func(st screencast.Stats) {
			total.Lock()
			total.updates += st.Updates
			total.keys += st.Keys
			total.bytes += st.Bytes
			total.encoder, total.route = st.Encoder, st.Route
			total.Unlock()
			advise(struct {
				Type   string `json:"type"`
				Path   string `json:"path"`
				Locked bool   `json:"locked"`
				screencast.Stats
			}{"stats", path, locked.Load(), st})
		},
		SignOffer:        signOffer,
		VerifyAnswer:     verifyAnswer,
		LoopbackOnly:     s.screenLoopbackOnly,
		ICEServers:       iceServers,
		ViewerICEServers: viewerICE,
		RelayMaxKbps:     relayMaxKbps,
	})
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
			message := "The connection to the screen was lost"
			switch {
			case errors.Is(session.Err(), screencast.ErrEncoder):
				message = screencast.ErrEncoder.Error()
			case watch:
				message = "The agent is done with the screen"
			}
			sendJSON(map[string]string{"type": "error", "error": message})
		}
	}
}
