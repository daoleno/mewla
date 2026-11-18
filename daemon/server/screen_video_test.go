package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// The computer's own screen as pushed video: the socket carries the offer,
// the answer and the candidates; the picture comes as H.264 over WebRTC and a
// press on the input channel reaches the screen.
func TestScreenViewerAsVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg: this computer has no H.264 encoder")
	}
	a, priv, device := sessionFileAuthFixture(t)
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(t.TempDir(), fake)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	if err := service.Enable(context.Background(), device, "Pixel"); err != nil {
		t.Fatal(err)
	}
	pressed := make(chan struct{}, 1)
	fake.AfterPointer = func(int) {
		select {
		case pressed <- struct{}{}:
		default:
		}
	}
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen-view"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer?video=webrtc", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	media := &webrtc.MediaEngine{}
	if err := media.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	viewer, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings), webrtc.WithMediaEngine(media)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	frames := make(chan struct{}, 64)
	viewer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			if pkt.Marker {
				select {
				case frames <- struct{}{}:
				default:
				}
			}
		}
	})
	input := make(chan *webrtc.DataChannel, 1)
	viewer.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() == "input" {
			dc.OnOpen(func() { input <- dc })
		}
	})
	writes := make(chan any, 16)
	go func() {
		for v := range writes {
			_ = conn.WriteJSON(v)
		}
	}()
	defer close(writes)
	viewer.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			writes <- map[string]any{"type": "ice", "candidate": c.ToJSON()}
		}
	})

	var ready, offered bool
	deadline := time.Now().Add(10 * time.Second)
	for !ready || !offered {
		_ = conn.SetReadDeadline(deadline)
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if kind == websocket.BinaryMessage {
			t.Fatal("RFB bytes on a video socket")
		}
		var msg struct {
			Type      string                   `json:"type"`
			SDPType   string                   `json:"sdp_type"`
			SDP       string                   `json:"sdp"`
			Candidate *webrtc.ICECandidateInit `json:"candidate"`
			Error     string                   `json:"error"`
		}
		_ = json.Unmarshal(data, &msg)
		switch msg.Type {
		case "error":
			t.Fatalf("the computer says %s", msg.Error)
		case "ready":
			ready = true
		case "rtc":
			if err := viewer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}); err != nil {
				t.Fatal(err)
			}
			answer, err := viewer.CreateAnswer(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := viewer.SetLocalDescription(answer); err != nil {
				t.Fatal(err)
			}
			writes <- map[string]any{"type": "rtc", "sdp_type": "answer", "sdp": answer.SDP}
			offered = true
		case "ice":
			if msg.Candidate != nil {
				_ = viewer.AddICECandidate(*msg.Candidate)
			}
		}
	}
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	select {
	case <-frames:
	case <-time.After(10 * time.Second):
		t.Fatal("no picture arrived")
	}
	var dc *webrtc.DataChannel
	select {
	case dc = <-input:
	case <-time.After(5 * time.Second):
		t.Fatal("no input channel")
	}
	msg := binary.BigEndian.AppendUint32(nil, 1)
	msg = append(msg, 5, 1, 0, 10, 0, 20) // press at (10, 20)
	if err := dc.Send(msg); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pressed:
	case <-time.After(5 * time.Second):
		t.Fatal("the press never reached the screen")
	}
	if pointers, _ := fake.Snapshot(); len(pointers) == 0 || pointers[0] != (screen.FakePointer{X: 10, Y: 20, Buttons: 1}) {
		t.Fatalf("pointers %+v", pointers)
	}
}
