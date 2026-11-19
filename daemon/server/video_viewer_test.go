package server

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/screencast"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// requireVideo skips a test on a computer with no H.264 encoder: no ffmpeg
// on Linux, or a Mac or Windows whose own encoder will not start (a CI VM).
func requireVideo(t *testing.T) {
	t.Helper()
	factory := screen.CastEncoder()
	if factory == nil {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			t.Skip("no ffmpeg: this computer has no H.264 encoder")
		}
		return
	}
	enc, err := factory(1000)
	if err == nil {
		defer enc.Close()
		pix := make([]byte, 320*240*4)
		_, err = enc.Encode(screencast.Picture{Pix: pix, Width: 320, Height: 240, Stride: 320 * 4, RGB: true, Seq: 1}, true)
	}
	if err != nil {
		t.Skipf("this %s's H.264 encoder does not start here: %v", runtime.GOOS, err)
	}
}

// videoViewer is a viewer of pushed video, as the app's viewer page is: it
// answers the computer's offer on the socket, plays nothing but counts the
// pictures, and sends input on the data channels.
type videoViewer struct {
	t      *testing.T
	id     videoIdentity
	conn   *websocket.Conn
	pc     *webrtc.PeerConnection
	writeM sync.Mutex
	frames chan struct{}
	input  chan *webrtc.DataChannel
	dc     *webrtc.DataChannel
	seq    uint32
	text   chan []byte
	closed chan error
}

// videoIdentity is who the viewer and its computer are: the device signs its
// answer's DTLS fingerprints (sign, a device authorization for a purpose),
// and the computer's offer is checked against its key (daemonKey).
type videoIdentity struct {
	sign      func(purpose string) string
	daemonKey string
}

func dialVideoViewer(t *testing.T, url string, id videoIdentity) *videoViewer {
	t.Helper()
	header := http.Header{}
	header.Set("Authorization", id.sign("mewla-display-view"))
	if strings.Contains(url, "/screen/viewer") {
		header.Set("Authorization", id.sign("mewla-screen-view"))
	}
	if strings.Contains(url, "?") {
		url += "&video=webrtc"
	} else {
		url += "?video=webrtc"
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	media := &webrtc.MediaEngine{}
	if err := media.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings), webrtc.WithMediaEngine(media)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	v := &videoViewer{t: t, id: id, conn: conn, pc: pc, frames: make(chan struct{}, 256), input: make(chan *webrtc.DataChannel, 1), text: make(chan []byte, 256), closed: make(chan error, 1)}
	t.Cleanup(v.close)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			if pkt.Marker {
				select {
				case v.frames <- struct{}{}:
				default:
				}
			}
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() == "input" {
			dc.OnOpen(func() { v.input <- dc })
		}
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			v.sendJSON(map[string]any{"type": "ice", "candidate": c.ToJSON()})
		}
	})
	go v.read()
	return v
}

// read answers the signalling and hands every other message to text.
func (v *videoViewer) read() {
	for {
		kind, data, err := v.conn.ReadMessage()
		if err != nil {
			v.closed <- err
			close(v.text)
			return
		}
		if kind == websocket.BinaryMessage {
			v.t.Errorf("binary message on a video socket: %d bytes", len(data))
			continue
		}
		var msg struct {
			Type      string                   `json:"type"`
			SDPType   string                   `json:"sdp_type"`
			SDP       string                   `json:"sdp"`
			Candidate *webrtc.ICECandidateInit `json:"candidate"`
			Assertion struct {
				Timestamp    string `json:"timestamp"`
				NonceHex     string `json:"nonce_hex"`
				SignatureHex string `json:"signature_hex"`
			} `json:"assertion"`
		}
		_ = json.Unmarshal(data, &msg)
		switch msg.Type {
		case "rtc":
			purpose := auth.ScreenDTLSPurpose(screencast.Fingerprints(msg.SDP))
			if !auth.VerifyServerAssertion(v.id.daemonKey, purpose, auth.ServerAssertion{Timestamp: msg.Assertion.Timestamp, NonceHex: msg.Assertion.NonceHex, SignatureHex: msg.Assertion.SignatureHex}) {
				v.t.Errorf("the offer's DTLS fingerprints are not signed by the computer")
				continue
			}
			if err := v.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}); err != nil {
				v.t.Errorf("offer: %v", err)
				continue
			}
			answer, err := v.pc.CreateAnswer(nil)
			if err == nil {
				err = v.pc.SetLocalDescription(answer)
			}
			if err != nil {
				v.t.Errorf("answer: %v", err)
				continue
			}
			v.sendJSON(map[string]any{"type": "rtc", "sdp_type": "answer", "sdp": answer.SDP,
				"authorization": v.id.sign(auth.ScreenDTLSPurpose(screencast.Fingerprints(answer.SDP)))})
		case "ice":
			if msg.Candidate != nil {
				_ = v.pc.AddICECandidate(*msg.Candidate)
			}
		default:
			select {
			case v.text <- data:
			default:
			}
		}
	}
}

func (v *videoViewer) sendJSON(m any) {
	v.writeM.Lock()
	defer v.writeM.Unlock()
	_ = v.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = v.conn.WriteJSON(m)
}

// picture waits for a picture to arrive.
func (v *videoViewer) picture() {
	v.t.Helper()
	select {
	case <-v.frames:
	case <-time.After(10 * time.Second):
		v.t.Fatal("no picture arrived")
	}
}

// send sends one RFB client message (a key, the pointer) on the reliable
// input channel, as the viewer page does.
func (v *videoViewer) send(msg []byte) {
	v.t.Helper()
	if v.dc == nil {
		select {
		case v.dc = <-v.input:
		case <-time.After(10 * time.Second):
			v.t.Fatal("no input channel")
		}
	}
	v.seq++
	if err := v.dc.Send(append(binary.BigEndian.AppendUint32(nil, v.seq), msg...)); err != nil {
		v.t.Fatal(err)
	}
}

// next is the next message of this type (pong, state, stats, error, reflow).
func (v *videoViewer) next(kind string) []byte {
	v.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case data, ok := <-v.text:
			if !ok {
				v.t.Fatalf("closed while waiting for %q: %v", kind, <-v.closed)
			}
			var head struct{ Type string }
			if json.Unmarshal(data, &head) == nil && head.Type == kind {
				return data
			}
		case <-deadline:
			v.t.Fatalf("no %q message", kind)
		}
	}
}

// errorText is the next error message's text.
func (v *videoViewer) errorText() string {
	v.t.Helper()
	var msg struct{ Error string }
	_ = json.Unmarshal(v.next("error"), &msg)
	return msg.Error
}

// closedWith waits for the socket to close and says why.
func (v *videoViewer) closedWith() error {
	v.t.Helper()
	for {
		select {
		case _, ok := <-v.text:
			if !ok {
				return <-v.closed
			}
		case <-time.After(10 * time.Second):
			v.t.Fatal("the socket stayed open")
		}
	}
}

func (v *videoViewer) close() {
	_ = v.pc.Close()
	_ = v.conn.Close()
}

// viewerIdentity is the test fixture's device, viewing the fixture's computer.
func viewerIdentity(priv ed25519.PrivateKey, a *auth.Manager, device string) videoIdentity {
	return videoIdentity{
		sign:      func(purpose string) string { return calendarAuthHeader(priv, a.DaemonID(), device, purpose) },
		daemonKey: a.PublicKeyHex(),
	}
}

// serveViewers serves handler and, when the test ends, waits for every
// handler to return. A viewer's socket is hijacked, so closing the server does
// not wait for it, and a handler still finishing (recording the device as
// seen when it verifies the viewer's answer) would write into the test's
// directories while they are removed.
func serveViewers(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	var running sync.WaitGroup
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		running.Add(1)
		defer running.Done()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		h.Close()
		done := make(chan struct{})
		go func() {
			running.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("a viewer's handler was still running 10s after the test")
		}
	})
	return h
}
