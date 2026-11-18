package screencast

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

type fakeSource struct {
	mu   sync.Mutex
	n    int
	w, h int
	ch   chan struct{}
}

func (f *fakeSource) Picture() ([]byte, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return testPicture(f.w, f.h, f.n), f.w, f.h
}

func (f *fakeSource) Watch() (<-chan struct{}, func()) { return f.ch, func() {} }

func (f *fakeSource) change() {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
	select {
	case f.ch <- struct{}{}:
	default:
	}
}

// A viewer peer (Pion standing in for the browser) gets the offer, answers,
// receives H.264 and sends input on both channels.
func TestSessionVideoAndInput(t *testing.T) {
	if _, _, err := findEncoder(); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	viewer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()

	toViewer := make(chan Signal, 64)
	var inputs [][]byte
	var inMu sync.Mutex
	gotInput := make(chan struct{}, 16)
	src := &fakeSource{w: 321, h: 241, ch: make(chan struct{}, 1)} // odd: the stream is cropped to even
	session, err := Start(ctx, Config{
		Source: src,
		Input: func(msg []byte) error {
			inMu.Lock()
			inputs = append(inputs, append([]byte(nil), msg...))
			inMu.Unlock()
			gotInput <- struct{}{}
			return nil
		},
		Signal:       func(v any) { toViewer <- v.(Signal) },
		LoopbackOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	frames := make(chan int, 64)
	viewer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Codec().MimeType != webrtc.MimeTypeH264 {
			t.Errorf("codec %s", track.Codec().MimeType)
		}
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			if pkt.Marker {
				frames <- len(pkt.Payload)
			}
		}
	})
	channels := make(chan *webrtc.DataChannel, 2)
	viewer.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() { channels <- dc })
	})
	viewer.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		raw, _ := json.Marshal(Signal{Type: "ice", Candidate: &init})
		_ = session.HandleSignal(raw)
	})
	go func() {
		for sig := range toViewer {
			switch sig.Type {
			case "rtc":
				if err := viewer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sig.SDP}); err != nil {
					t.Error(err)
					return
				}
				answer, _ := viewer.CreateAnswer(nil)
				_ = viewer.SetLocalDescription(answer)
				raw, _ := json.Marshal(Signal{Type: "rtc", SDPType: "answer", SDP: answer.SDP})
				if err := session.HandleSignal(raw); err != nil {
					t.Error(err)
				}
			case "ice":
				_ = viewer.AddICECandidate(*sig.Candidate)
			}
		}
	}()

	// The first picture comes without any change: a key frame once connected.
	select {
	case <-frames:
	case <-ctx.Done():
		t.Fatal("no picture")
	}
	src.change()
	select {
	case <-frames:
	case <-ctx.Done():
		t.Fatal("no picture after a change")
	}

	byLabel := map[string]*webrtc.DataChannel{}
	for len(byLabel) < 2 {
		select {
		case dc := <-channels:
			byLabel[dc.Label()] = dc
		case <-ctx.Done():
			t.Fatal("data channels did not open")
		}
	}
	reliable := func(seq uint32, msg ...byte) {
		b := binary.BigEndian.AppendUint32(nil, seq)
		_ = byLabel["input"].Send(append(b, msg...))
	}
	move := func(seq uint32, x, y uint16) {
		b := binary.BigEndian.AppendUint32(nil, seq)
		b = binary.BigEndian.AppendUint16(b, x)
		_ = byLabel["pointer"].Send(binary.BigEndian.AppendUint16(b, y))
	}
	reliable(1, 5, 1, 0, 10, 0, 20) // press at 10,20
	<-gotInput
	move(3, 30, 40) // a move while pressed carries the button
	<-gotInput
	move(2, 99, 99) // unordered: arrives after seq 3 but is newer than the last reliable one, so it applies
	<-gotInput
	reliable(5, 5, 0, 0, 30, 0, 40, 4, 1, 0, 0, 0, 0, 0, 0x61) // release, then 'a' down
	<-gotInput
	<-gotInput
	move(4, 1, 1) // sent before the release: dropped
	reliable(6, 4, 0, 0, 0, 0, 0, 0, 0x61)
	<-gotInput

	inMu.Lock()
	defer inMu.Unlock()
	want := [][]byte{
		{5, 1, 0, 10, 0, 20},
		{5, 1, 0, 30, 0, 40},
		{5, 1, 0, 99, 0, 99},
		{5, 0, 0, 30, 0, 40},
		{4, 1, 0, 0, 0, 0, 0, 0x61},
		{4, 0, 0, 0, 0, 0, 0, 0x61},
	}
	if len(inputs) != len(want) {
		t.Fatalf("inputs %v, want %v", inputs, want)
	}
	for i := range want {
		if string(inputs[i]) != string(want[i]) {
			t.Fatalf("input %d: %v, want %v", i, inputs[i], want[i])
		}
	}
}
