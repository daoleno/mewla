package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/phone"
	"github.com/gorilla/websocket"
)

// SetPhone installs the one phone connection agents use.
func (s *Server) SetPhone(m *phone.Manager) { s.phone = m }

var phoneUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
}

const (
	// A phone's answer can carry a JPEG of its screen.
	phoneReadLimit = 16 << 20
	// The phone says hello within this long of connecting.
	phoneHelloWait = 10 * time.Second
	// A phone that answers neither frames nor pings for this long is gone.
	phoneSilenceLimit = 3 * time.Minute / 2
	phoneWriteWait    = 15 * time.Second
)

// phonePingEvery is how often the daemon pings the phone and checks that its device is still
// paired.
const phonePingEvery = 20 * time.Second

// phoneAction is one change from the app. Exactly one field is set.
type phoneAction struct {
	// Enabled is the Phone use switch (Settings → Labs).
	Enabled *bool `json:"enabled,omitempty"`
	// StopAgent ends what the agent is doing on the phone, as the Stop button does.
	StopAgent bool `json:"stop_agent,omitempty"`
}

// handlePhone reports the phone and Phone use (GET), or changes the switch or stops the agent
// (POST). A change binds its signature to the body, like /agent-use.
func (s *Server) handlePhone(w http.ResponseWriter, r *http.Request) {
	if s.phone == nil || s.agentUse == nil {
		http.Error(w, "phone use unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-phone:GET:/phone"); !ok {
			return
		}
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, "mewla-phone:POST:/phone:sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var action phoneAction
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&action); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		switch {
		case action.StopAgent && action.Enabled == nil:
			// Stopping an agent is always safe, so any paired device may.
			s.phone.Stop()
		case action.Enabled != nil && !action.StopAgent:
			if _, err := s.agentUse.SetPhoneEnabled(*action.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if !*action.Enabled {
				// Off means now: an agent using the phone stops at once.
				s.phone.Release()
			}
		default:
			http.Error(w, "send one of enabled or stop_agent", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(agentuse.PhoneStatus{Enabled: s.agentUse.PhoneEnabled(), Status: s.phone.Status()})
}

// phoneConn is the daemon's end of the phone's WebSocket for the manager: one writer at a time,
// whatever sends.
type phoneConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *phoneConn) Send(frame []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(phoneWriteWait))
	return c.conn.WriteMessage(websocket.TextMessage, frame)
}

// Close may come from any goroutine: control frames are the one thing a WebSocket lets another
// goroutine write.
func (c *phoneConn) Close(code int, reason string) {
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	_ = c.conn.Close()
}

// handlePhoneDevice is the phone's one WebSocket to this computer: JSON text frames, the phone's
// hello first. A newer connection from a paired device replaces the older one.
func (s *Server) handlePhoneDevice(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateRequest(w, r, "mewla-phone-device")
	if !ok {
		return
	}
	if s.phone == nil {
		http.Error(w, "phone use unavailable", http.StatusServiceUnavailable)
		return
	}
	ws, err := phoneUpgrader.Upgrade(w, r, websocketAuthResponseHeader(r))
	if err != nil {
		return
	}
	conn := &phoneConn{conn: ws}
	defer ws.Close()
	ws.SetReadLimit(phoneReadLimit)

	var hello phone.Hello
	_ = ws.SetReadDeadline(time.Now().Add(phoneHelloWait))
	kind, data, err := ws.ReadMessage()
	if err != nil || kind != websocket.TextMessage || json.Unmarshal(data, &hello) != nil || hello.Type != phone.TypeHello {
		conn.Close(phone.CloseProtocol, "The first message must be the phone's hello.")
		return
	}
	link, err := s.phone.Attach(hello, conn)
	if err != nil {
		conn.Close(phone.CloseUnsupported, err.Error())
		return
	}
	defer link.Detach()
	log.Printf("phone: device %s connected (%s, Android API %d, state %s)", device.ID, hello.Model, hello.SDK, hello.State)
	defer log.Printf("phone: device %s disconnected", device.ID)

	// Pings keep the connection honest through NATs and tunnels, and tell a dead phone from a
	// quiet one; a phone that is unpaired meanwhile is let go.
	alive := func() { _ = ws.SetReadDeadline(time.Now().Add(phoneSilenceLimit)) }
	alive()
	ws.SetPongHandler(func(string) error { alive(); return nil })
	pingEvery := phonePingEvery
	if s.phonePing > 0 {
		pingEvery = s.phonePing
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(pingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if s.auth != nil && !s.auth.IsDeviceTrusted(device.ID) {
					conn.Close(websocket.ClosePolicyViolation, "This device is no longer paired.")
					return
				}
				if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)) != nil {
					return
				}
			}
		}
	}()

	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		alive()
		if kind != websocket.TextMessage {
			continue
		}
		if err := link.Receive(data); err != nil {
			conn.Close(phone.CloseProtocol, "A message was not valid JSON.")
			return
		}
	}
}
