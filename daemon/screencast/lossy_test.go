package screencast

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/transport/v5/vnet"
	"github.com/pion/webrtc/v4"
)

// thinLink is a virtual link of about kbps, with loss and a round trip, as a
// phone on a weak mobile network has.
func thinLink(t *testing.T, kbps int, loss float64, rtt time.Duration) (computer, viewer *vnet.Net) {
	t.Helper()
	wan, err := vnet.NewRouter(&vnet.RouterConfig{CIDR: "1.2.3.0/24", MinDelay: rtt / 2, LoggerFactory: logging.NewDefaultLoggerFactory()})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	rate := float64(kbps) * 1000 / 8
	tokens, at := rate/4, time.Now()
	wan.AddChunkFilter(func(c vnet.Chunk) bool {
		if rand.Float64() < loss {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		tokens = min(rate/4, tokens+now.Sub(at).Seconds()*rate)
		at = now
		n := float64(len(c.UserData()) + 28)
		if tokens < n {
			return false // the link is full: a router queue overflowing
		}
		tokens -= n
		return true
	})
	if computer, err = vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{"1.2.3.4"}}); err != nil {
		t.Fatal(err)
	}
	if viewer, err = vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{"1.2.3.5"}}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*vnet.Net{computer, viewer} {
		if err := wan.AddNet(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := wan.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wan.Stop() })
	return computer, viewer
}

// On a thin, lossy link (1.5 Mbit/s, 2 % loss, 150 ms round trip) the stream
// settles: the rate comes under the link, key frames stay about one a second
// at most, and moving pictures keep arriving.
func TestSessionOnAThinLossyLink(t *testing.T) {
	if testing.Short() {
		t.Skip("20 s")
	}
	if _, _, err := findEncoder(); err != nil {
		t.Skip(err)
	}
	computerNet, viewerNet := thinLink(t, 1500, 0.02, 150*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	var se webrtc.SettingEngine
	se.SetNet(viewerNet)
	viewer, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	var frames atomic.Int64
	viewer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// The viewer asks for a key frame when it loses one, as a browser does.
		go func() {
			for ctx.Err() == nil {
				if _, _, err := receiver.ReadRTCP(); err != nil {
					return
				}
			}
		}()
		var lastSeq uint16
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			if lastSeq != 0 && pkt.SequenceNumber != lastSeq+1 {
				_ = viewer.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
			}
			lastSeq = pkt.SequenceNumber
			if pkt.Marker {
				frames.Add(1)
			}
		}
	})
	toViewer := make(chan Signal, 64)
	var mu sync.Mutex
	var last Stats
	var keys int
	src := &fakeSource{w: 640, h: 400, ch: make(chan struct{}, 1)}
	session, err := Start(ctx, Config{
		Source: src,
		Signal: func(v any) { toViewer <- v.(Signal) },
		Stats: func(s Stats) {
			mu.Lock()
			last = s
			keys += s.Keys
			mu.Unlock()
		},
		net: computerNet,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	go answerAll(ctx, t, session, viewer, toViewer)

	// Ten seconds of motion at 30 pictures a second, after the link settles.
	stop := time.After(20 * time.Second)
	tick := time.NewTicker(33 * time.Millisecond)
	defer tick.Stop()
	var settledFrames int64
	var settledKeys int
	settle := time.After(10 * time.Second)
loop:
	for {
		select {
		case <-tick.C:
			src.change()
		case <-settle:
			settledFrames = frames.Load()
			mu.Lock()
			settledKeys = keys
			mu.Unlock()
		case <-stop:
			break loop
		case <-ctx.Done():
			t.Fatal("timed out")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	fps := float64(frames.Load()-settledFrames) / 10
	keysPerSecond := float64(keys-settledKeys) / 10
	t.Logf("settled: %.1f pictures/s, %.1f key frames/s, target %d kbit/s, route %s", fps, keysPerSecond, last.Kbps, last.Route)
	if last.Kbps > 1500 {
		t.Errorf("target %d kbit/s on a 1500 kbit/s link", last.Kbps)
	}
	if keysPerSecond > 1.2 {
		t.Errorf("%.1f key frames a second", keysPerSecond)
	}
	if fps < 8 {
		t.Errorf("%.1f pictures a second", fps)
	}
}

// answerAll answers the session's offer and trades candidates, as the viewer page does.
func answerAll(ctx context.Context, t *testing.T, session *Session, viewer *webrtc.PeerConnection, toViewer chan Signal) {
	viewer.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			init := c.ToJSON()
			raw, _ := json.Marshal(Signal{Type: "ice", Candidate: &init})
			_ = session.HandleSignal(raw)
		}
	})
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-toViewer:
			switch sig.Type {
			case "rtc":
				if err := viewer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sig.SDP}); err != nil {
					t.Error(err)
					return
				}
				answer, err := viewer.CreateAnswer(nil)
				if err == nil {
					err = viewer.SetLocalDescription(answer)
				}
				if err != nil {
					t.Error(err)
					return
				}
				raw, _ := json.Marshal(Signal{Type: "rtc", SDPType: "answer", SDP: answer.SDP})
				_ = session.HandleSignal(raw)
			case "ice":
				if sig.Candidate != nil {
					_ = viewer.AddICECandidate(*sig.Candidate)
				}
			}
		}
	}
}
