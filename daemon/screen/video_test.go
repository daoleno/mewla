package screen

import (
	"context"
	"encoding/binary"
	"image"
	"os"
	"sync"
	"testing"
	"time"
)

// fakeEncoder stands in for a hardware encoder: a key frame is 40 KB and a
// delta 6 KB of a recognisable fill, every frame led by an access unit
// delimiter, so the protocol around it can be checked without a codec.
type fakeEncoder struct {
	mu      sync.Mutex
	frames  int
	keys    int
	kbps    []int
	closed  bool
	forceAt []int
}

func (f *fakeEncoder) Encode(img *image.RGBA, key bool) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	isKey := key || f.frames == 0
	f.frames++
	size := 6 << 10
	if isKey {
		size = 40 << 10
		f.keys++
	}
	out := make([]byte, size)
	copy(out, []byte{0, 0, 0, 1, 0x09, 0xf0})
	return out, isKey, nil
}
func (f *fakeEncoder) SetBitrate(kbps int) { f.mu.Lock(); f.kbps = append(f.kbps, kbps); f.mu.Unlock() }
func (f *fakeEncoder) Close()              { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}

func init() { os.Setenv("MEWLA_SCREEN_VIDEO", "on") }

func fakeVideo(enc *fakeEncoder) videoFactory {
	return func(w, h, kbps int) (videoEncoder, error) { return enc, nil }
}

func TestVideoPathEntersOnTwoBigUpdatesAndLeavesWhenQuiet(t *testing.T) {
	v := newVideoPath(fakeVideo(&fakeEncoder{}))
	now := time.Unix(1000, 0)
	const total = 1000
	if v.choose(now, 300, total, false) {
		t.Fatal("one big update started video")
	}
	if v.choose(now, 100, total, false) {
		t.Fatal("a small update after a big one counted")
	}
	if v.choose(now, 300, total, false) {
		t.Fatal("big updates that are not consecutive started video")
	}
	if !v.choose(now, 300, total, false) {
		t.Fatal("two big updates in a row did not start video")
	}
	// A few small changes do not leave video at once ...
	if !v.choose(now.Add(100*time.Millisecond), 10, total, false) {
		t.Fatal("left video on the first quiet update")
	}
	// ... but a quiet stretch does, and motion in between restarts the clock.
	if !v.choose(now.Add(300*time.Millisecond), 300, total, false) {
		t.Fatal("motion ended video")
	}
	if !v.choose(now.Add(500*time.Millisecond), 10, total, false) {
		t.Fatal("quiet clock was not restarted by motion")
	}
	if v.choose(now.Add(500*time.Millisecond+videoQuiet), 10, total, false) {
		t.Fatal("still in video after a quiet stretch")
	}
}

func TestVideoPathNeedsAnEncoderAndSwitchOff(t *testing.T) {
	t.Setenv("MEWLA_SCREEN_VIDEO", "on")
	if newVideoPath(nil) != nil {
		t.Fatal("a build without an encoder has a video path")
	}
	t.Setenv("MEWLA_SCREEN_VIDEO", "off")
	if newVideoPath(fakeVideo(&fakeEncoder{})) != nil {
		t.Fatal("MEWLA_SCREEN_VIDEO=off left video on")
	}
}

func TestVideoRateFallsUnderWhatGotThroughAndRisesWhenCalm(t *testing.T) {
	r := newVideoRate()
	now := time.Unix(2000, 0)
	// A calm link at a 60 ms round trip: no change, then a rise.
	for i := 0; i < 90; i++ {
		now = now.Add(33 * time.Millisecond)
		r.observe(now, 60*time.Millisecond, 6<<10)
	}
	if r.current() <= videoStartKbps {
		t.Fatalf("a calm link did not raise the rate: %d", r.current())
	}
	high := r.current()
	// Answers now arrive a second late and about 1 Mbit/s is getting through.
	for i := 0; i < 60; i++ {
		now = now.Add(33 * time.Millisecond)
		r.observe(now, 1100*time.Millisecond, 4<<10)
	}
	if r.current() >= high || r.current() < videoMinKbps {
		t.Fatalf("a late link left the rate at %d (was %d)", r.current(), high)
	}
	if r.current() > 1100 {
		t.Fatalf("rate %d kbit/s is above what got through", r.current())
	}
}

// A viewer that offers H.264 gets motion as video and a sharp still after it;
// one that does not, and the agent's Watch, only ever get tiles.
func TestMotionGoesAsVideoOnlyToAViewerThatOffersIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		offer   bool
		factory func(*fakeEncoder) videoFactory
		video   bool
	}{
		{"offers H.264", true, fakeVideo, true},
		{"does not offer H.264", false, fakeVideo, false},
		{"build without an encoder", true, func(*fakeEncoder) videoFactory { return nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc := &fakeEncoder{}
			fake := NewFakeBackend(640, 400)
			fake.Scroll = 6
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, _ := fake.Start(ctx)
			viewerConn, server := netPipe()
			done := make(chan struct{})
			go func() {
				_ = serveRFBVideo(ctx, server, session, "Test", &Observer{}, tc.factory(enc))
				close(done)
			}()
			v := &testViewer{t: t, conn: viewerConn, fb: image.NewRGBA(image.Rect(0, 0, 640, 400))}
			v.write([]byte{1})
			head := v.read(24)
			v.read(int(binary.BigEndian.Uint32(head[20:])))
			v.write(append([]byte{msgSetPixelFormat, 0, 0, 0}, novncFormat.bytes()...))
			encs := []int32{encTight, encRaw, pseudoQualityLevel0 + 6, pseudoFence, pseudoContinuousUpdts}
			if tc.offer {
				encs = append([]int32{encH264}, encs...)
			}
			v.setEncodings(encs...)
			v.next() // continuous updates announced
			v.write([]byte{msgEnableContinuous, 1, 0, 0, 0, 0, 2, 128, 1, 144})

			var videoRects, tileRects, keys int
			var firstFlags []uint32
			scrolling := true
			stop := time.After(1500 * time.Millisecond)
			go func() {
				for scrolling {
					time.Sleep(time.Second / 30)
					fake.ScrollTick()
				}
			}()
			func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					msg := v.next()
					if msg.kind == msgFence {
						v.write(append([]byte{msgFence, 0, 0, 0, 0, 0, 0, 0, byte(len(msg.fence))}, msg.fence...))
						continue
					}
					for i, e := range msg.encs {
						switch e {
						case encH264:
							videoRects++
							firstFlags = append(firstFlags, msg.videoFlags[len(msg.videoFlags)-len(msg.encs)+i])
						case encTight:
							tileRects++
						}
					}
				}
			}()
			scrolling = false
			_ = keys
			if tc.video {
				if videoRects < 5 {
					t.Fatalf("scrolling produced %d video frames", videoRects)
				}
				if firstFlags[0]&h264ResetContext == 0 {
					t.Fatalf("the first video frame did not reset the decoder: flags %d", firstFlags[0])
				}
				for _, f := range firstFlags[1:] {
					if f != 0 {
						t.Fatalf("a later frame reset the decoder: flags %d", f)
					}
				}
				enc.mu.Lock()
				frames, ekeys := enc.frames, enc.keys
				enc.mu.Unlock()
				if ekeys != 1 || frames != videoRects {
					t.Fatalf("encoder saw %d frames and %d keys for %d rects", frames, ekeys, videoRects)
				}
			} else if videoRects != 0 {
				t.Fatalf("%d video frames to a viewer that cannot show them", videoRects)
			}
			if tileRects == 0 {
				t.Fatal("no tiles at all")
			}
			viewerConn.Close()
			<-done
			if tc.video {
				enc.mu.Lock()
				defer enc.mu.Unlock()
				if !enc.closed {
					t.Fatal("the encoder outlived the session")
				}
			}
		})
	}
}
