package screencast

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// A view that starts on a thin link (1.5 Mbit/s behind an 800 ms queue, 150 ms
// round trip, an uplink of its own) of a screen in full motion keeps moving: the video comes under
// the link even while the link's feedback is too late for the congestion
// control to read, and the viewer decodes pictures, not a frozen first one.
func TestSessionStartingOnAThinLinkKeepsMoving(t *testing.T) {
	if testing.Short() {
		t.Skip("25 s")
	}
	if _, _, err := findEncoder(); err != nil {
		t.Skip(err)
	}
	computerNet, viewerNet := thinLink(t, 1500, 150_000, 0, 150*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// The viewer as a browser is: NACK, receiver reports and transport-wide
	// feedback.
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	reg := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, reg); err != nil {
		t.Fatal(err)
	}
	var se webrtc.SettingEngine
	se.SetNet(viewerNet)
	viewer, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(reg), webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	// Pictures the viewer can decode: whole, since an unbroken key frame.
	var decodable atomic.Int64
	viewer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go func() {
			for ctx.Err() == nil {
				if _, _, err := receiver.ReadRTCP(); err != nil {
					return
				}
			}
		}()
		asm := newAssembler()
		var lastPLI time.Time
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			n, lost := asm.add(pkt.SequenceNumber, pkt.Timestamp, pkt.Marker, startsKeyFrame(pkt.Payload), time.Now())
			decodable.Add(int64(n))
			if lost && time.Since(lastPLI) > 500*time.Millisecond {
				lastPLI = time.Now()
				_ = viewer.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
			}
		}
	})
	toViewer := make(chan Signal, 64)
	var mu sync.Mutex
	var last Stats
	var sent int
	src := &fakeSource{w: 960, h: 600, ch: make(chan struct{}, 1)}
	session, err := Start(ctx, Config{
		Source: src,
		Signal: func(v any) { toViewer <- v.(Signal) },
		Stats: func(s Stats) {
			mu.Lock()
			last = s
			sent += s.Bytes
			mu.Unlock()
		},
		net: computerNet,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	go answerAll(ctx, t, session, viewer, toViewer)

	stop := time.After(25 * time.Second)
	tick := time.NewTicker(16 * time.Millisecond)
	defer tick.Stop()
	var settledPictures int64
	var settledBytes int
	settle := time.After(10 * time.Second)
loop:
	for {
		select {
		case <-tick.C:
			src.change()
		case <-settle:
			settledPictures = decodable.Load()
			mu.Lock()
			settledBytes = sent
			mu.Unlock()
		case <-stop:
			break loop
		case <-ctx.Done():
			t.Fatal("timed out")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	fps := float64(decodable.Load()-settledPictures) / 15
	kbps := float64(sent-settledBytes) * 8 / 1000 / 15
	t.Logf("settled: %.1f decodable pictures/s, %.0f kbit/s sent, target %d kbit/s", fps, kbps, last.Kbps)
	if kbps > 1500 {
		t.Errorf("%.0f kbit/s sent into a 1500 kbit/s link", kbps)
	}
	if fps < 2 {
		t.Errorf("%.1f decodable pictures a second", fps)
	}
}

// startsKeyFrame reports whether an H.264 RTP payload begins a key frame: an
// SPS, alone, in an aggregate or as the first fragment.
func startsKeyFrame(p []byte) bool {
	if len(p) < 2 {
		return false
	}
	switch p[0] & 0x1f {
	case 7, 5:
		return true
	case 24: // STAP-A: the first aggregated unit
		return len(p) > 3 && (p[3]&0x1f == 7 || p[3]&0x1f == 5)
	case 28: // FU-A: its start fragment
		return p[1]&0x80 != 0 && (p[1]&0x1f == 7 || p[1]&0x1f == 5)
	}
	return false
}

// assembler puts a viewer's packets back into pictures as a browser's jitter
// buffer does: a picture is decodable when all its packets came (a resent one
// too) within a second, and the picture before it was, or it is a key frame.
type assembler struct {
	got      map[uint16]bool
	frames   []*assembled // in the order they began
	byStamp  map[uint32]*assembled
	prevEnd  uint16
	prevKnow bool
	chain    bool
}

type assembled struct {
	stamp      uint32
	first, end uint16
	ended, key bool
	at         time.Time
}

func newAssembler() *assembler {
	return &assembler{got: map[uint16]bool{}, byStamp: map[uint32]*assembled{}}
}

// add takes one packet; it returns the pictures that became decodable and
// whether one was lost (the viewer then asks for a key frame).
func (a *assembler) add(seq uint16, stamp uint32, marker, key bool, now time.Time) (decodable int, lost bool) {
	a.got[seq] = true
	f := a.byStamp[stamp]
	if f == nil {
		f = &assembled{stamp: stamp, first: seq, at: now}
		a.byStamp[stamp] = f
		a.frames = append(a.frames, f)
	} else if int16(seq-f.first) < 0 {
		f.first = seq
	}
	if marker {
		f.end, f.ended = seq, true
	}
	if key {
		f.key = true
	}
	for len(a.frames) > 0 {
		f := a.frames[0]
		start := f.first
		if a.prevKnow && !f.key {
			start = a.prevEnd + 1
		}
		whole := f.ended && (a.prevKnow || f.key)
		for s := start; whole && s != f.end+1; s++ {
			whole = a.got[s]
		}
		switch {
		case whole:
			a.chain = a.chain || f.key
			if a.chain {
				decodable++
			}
		case now.Sub(f.at) > time.Second:
			a.chain, lost = false, true
		default:
			return decodable, lost
		}
		if f.ended {
			a.prevEnd, a.prevKnow = f.end, true
		} else {
			a.prevKnow = false
		}
		for s := f.first; s != f.end+1 && f.ended; s++ {
			delete(a.got, s)
		}
		delete(a.byStamp, f.stamp)
		a.frames = a.frames[1:]
	}
	return decodable, lost
}
