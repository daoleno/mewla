package screen

import (
	"context"
	"encoding/binary"
	"image"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestHistogramQuantilesAreBucketBounds(t *testing.T) {
	var h histogram
	if h.quantile(0.5) != 0 {
		t.Fatal("an empty histogram has a quantile")
	}
	for i := 0; i < 90; i++ {
		h.add(9.4)
	}
	for i := 0; i < 10; i++ {
		h.add(700)
	}
	if got := h.quantile(0.5); got != 10 {
		t.Fatalf("p50 = %v, want its bucket's bound 10", got)
	}
	if got := h.quantile(0.95); got != 700 {
		t.Fatalf("p95 = %v, want its bucket's bound capped at the largest sample, 700", got)
	}
	h.add(9000)
	if h.quantile(1) != 9000 {
		t.Fatalf("the last bucket reports the largest sample, got %v", h.quantile(1))
	}
}

// simulated is a link as the controller sees it: updates leave one frame
// apart when the window allows, cross at rate bytes a second, and come back
// as acknowledgements one round trip later.
type simulated struct {
	rtt  time.Duration
	rate float64 // bytes per second
	// full is an update's size at the viewer's quality; each level below
	// is stepGrowth smaller.
	full int
}

// run streams for the given time and reports the updates acknowledged a
// second over its last half, with the controller left as it settled.
func (l simulated) run(c *streamController, viewer int, seconds int) (fps float64) {
	now := time.Unix(1000, 0)
	end := now.Add(time.Duration(seconds) * time.Second)
	type flight struct {
		sent, done time.Time
		bytes      int
	}
	var out []flight
	var linkFree, nextFrame time.Time
	linkFree, nextFrame = now, now
	acked := 0
	for ; now.Before(end); now = now.Add(time.Millisecond) {
		for len(out) > 0 && !out[0].done.After(now) {
			f := out[0]
			out = out[1:]
			c.observe(now, now.Sub(f.sent), f.bytes)
			if now.After(end.Add(-time.Duration(seconds) * time.Second / 2)) {
				acked++
			}
		}
		if len(out) < c.limit() && !now.Before(nextFrame) {
			size := int(float64(l.full) / math.Pow(stepGrowth, float64(viewer-c.quality(viewer))))
			start := linkFree
			if now.After(start) {
				start = now
			}
			linkFree = start.Add(time.Duration(float64(size) / l.rate * float64(time.Second)))
			out = append(out, flight{sent: now, done: linkFree.Add(l.rtt + 8*time.Millisecond), bytes: size})
			nextFrame = now.Add(33 * time.Millisecond)
		}
	}
	return float64(acked) / (float64(seconds) / 2)
}

func fixedController() *streamController {
	c := newStreamController()
	c.fixed = true
	return c
}

// A link that keeps up is left alone.
func TestAFastLinkIsLeftAlone(t *testing.T) {
	link := simulated{rtt: 4 * time.Millisecond, rate: 12.5e6, full: 200 << 10}
	c := newStreamController()
	fps := link.run(c, 6, 20)
	if c.quality(6) != 6 || c.limit() != inFlightBase {
		t.Fatalf("a fast link got quality %d, window %d", c.quality(6), c.limit())
	}
	if want := link.run(fixedController(), 6, 20); fps < 0.95*want {
		t.Fatalf("%.1f fps against %.1f fixed", fps, want)
	}
}

// A long round trip over a fat pipe is cured by a wider window: more updates
// leave before the first answer returns. The probe keeps the window because
// the viewer then gets more updates a second.
func TestARoundTripBoundLinkGetsAWiderWindow(t *testing.T) {
	link := simulated{rtt: 150 * time.Millisecond, rate: 6e6, full: 40 << 10}
	c := newStreamController()
	fps := link.run(c, 6, 30)
	fixed := link.run(fixedController(), 6, 30)
	if c.limit() != inFlightMax {
		t.Fatalf("window = %d, want %d", c.limit(), inFlightMax)
	}
	if fps < 1.5*fixed {
		t.Fatalf("%.1f fps against %.1f fixed: the wider window did not help", fps, fixed)
	}
	if c.quality(6) != 6 {
		t.Fatalf("a round-trip-bound link lost quality: %d", c.quality(6))
	}
}

// Where the bytes are what doesn't fit, more window only queues them: the
// probe fails and the window goes back, and quality falls instead.
func TestAByteBoundLinkLowersTheQuality(t *testing.T) {
	link := simulated{rtt: 60 * time.Millisecond, rate: 500e3, full: 200 << 10}
	c := newStreamController()
	fps := link.run(c, 6, 40)
	fixed := link.run(fixedController(), 6, 40)
	if c.quality(6) >= 6 {
		t.Fatalf("quality held at %d with bytes queueing", c.quality(6))
	}
	if c.limit() != inFlightBase {
		t.Fatalf("window = %d on a byte-bound link", c.limit())
	}
	if fps < 1.3*fixed {
		t.Fatalf("%.1f fps against %.1f fixed: lower quality did not help", fps, fixed)
	}
}

// When the link recovers, sharpness comes back a level at a time.
func TestQualityComesBackWhenTheLinkRecovers(t *testing.T) {
	c := newStreamController()
	now := time.Unix(1000, 0)
	for i := 0; i < 80; i++ {
		now = now.Add(100 * time.Millisecond)
		c.observe(now, 400*time.Millisecond, 120<<10)
	}
	if c.quality(9) >= 9 {
		t.Fatal("quality held under load")
	}
	if c.quality(9) < qualityFloor || c.quality(1) != 1 {
		t.Fatalf("quality %d / %d outside its bounds", c.quality(9), c.quality(1))
	}
	// 90 ms: a level up would take ~108 ms, still late.
	low := c.quality(9)
	for i := 0; i < 60; i++ {
		now = now.Add(100 * time.Millisecond)
		c.observe(now, 90*time.Millisecond, 30<<10)
	}
	if c.quality(9) != low {
		t.Fatalf("quality moved from %d to %d on a link only just keeping up", low, c.quality(9))
	}
	for i := 0; i < 20; i++ {
		now = now.Add(100 * time.Millisecond)
		c.observe(now, 40*time.Millisecond, 30<<10)
	}
	if got := c.quality(9); got <= low || got >= 9 {
		t.Fatalf("quality = %d after 2 s calm, want a level or two back from %d", got, low)
	}
	for i := 0; i < 400; i++ {
		now = now.Add(100 * time.Millisecond)
		c.observe(now, 40*time.Millisecond, 30<<10)
	}
	if c.quality(9) != 9 || c.limit() != inFlightBase {
		t.Fatalf("quality %d window %d after a long calm", c.quality(9), c.limit())
	}
}

func TestTileSetTracksWhatWasSentBlurry(t *testing.T) {
	bounds := image.Rect(0, 0, 200, 130)
	set := newTileSet(bounds)
	if set.any() || len(set.rects()) != 0 {
		t.Fatal("a new set holds tiles")
	}
	set.add(image.Rect(0, 0, 130, 70))
	if got := set.rects(); len(got) != 1 || got[0] != image.Rect(0, 0, 192, 128) {
		t.Fatalf("rects = %v", got)
	}
	// Covering part of a tile leaves it stale; covering all of it clears it.
	set.remove(image.Rect(0, 0, 64, 32))
	if !set.any() || len(set.rects()) == 0 {
		t.Fatal("a partly redrawn tile was forgotten")
	}
	set.remove(bounds)
	if set.any() || len(set.rects()) != 0 {
		t.Fatalf("tiles remain after a full redraw: %v", set.rects())
	}
	// The last, partial tiles are whole once the frame's edge covers them.
	set.add(image.Rect(192, 128, 200, 130))
	set.remove(image.Rect(192, 128, 200, 130))
	if set.any() {
		t.Fatal("an edge tile could not be cleared")
	}
}

// noisyFrame is a screen whose JPEG size follows its quality.
func noisyFrame(seed int64, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 255
	}
	return img
}

type sentUpdates struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (s *sentUpdates) write(b []byte) error {
	s.mu.Lock()
	s.msgs = append(s.msgs, append([]byte(nil), b...))
	s.mu.Unlock()
	return nil
}

func (s *sentUpdates) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs)
}

func (s *sentUpdates) at(i int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msgs[i]
}

// While the viewer is behind, motion goes out at a lower quality; once the
// screen is still, the same tiles go out again at the viewer's, and then
// nothing more is sent.
func TestAStillScreenIsSentSharpAfterMotionWentOutBlurry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &rfbState{
		format: novncFormat, tight: true, quality: 9, fence: true, continuous: true,
		wake: make(chan struct{}, 1), sendLog: map[uint64]sentRecord{},
		ctl: newStreamController(), metrics: &streamMetrics{},
	}
	state.ctl.drop = 6
	frames := &frameStore{wake: make(chan struct{}, 1)}
	frames.put(Frame{Seq: 1, Image: noisyFrame(1, 128, 128), At: time.Now()})
	var out sentUpdates
	go func() { _ = writeUpdates(ctx, state, frames, out.write) }()

	waitFor(t, func() bool { return out.count() >= 1 })
	blurry := len(out.at(0))
	// Acknowledge, as the viewer does, so the window opens for the refinement.
	state.mu.Lock()
	state.acked = state.sent
	state.mu.Unlock()
	state.signal()
	waitFor(t, func() bool { return out.count() >= 2 })
	sharp := len(out.at(1))
	if sharp < 2*blurry {
		t.Fatalf("sharpened update is %d bytes against %d blurry: no better quality", sharp, blurry)
	}
	if got := binary.BigEndian.Uint16(out.at(1)[2:]); got != 1 {
		t.Fatalf("sharpened update has %d rects", got)
	}
	state.mu.Lock()
	state.acked = state.sent
	state.mu.Unlock()
	state.signal()
	time.Sleep(2 * stillAfter)
	if out.count() != 2 {
		t.Fatalf("%d updates: a sharp screen was sent again", out.count())
	}
	if s := state.metrics.summary(); s.Refines != 1 || s.LowestQuality != 3 || s.ViewerQuality != 9 {
		t.Fatalf("summary %+v", s)
	}
}

// A moving screen is never held back to sharpen: the newest frame goes first.
func TestMotionBeatsSharpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &rfbState{
		format: novncFormat, tight: true, quality: 9, continuous: true,
		wake: make(chan struct{}, 1), sendLog: map[uint64]sentRecord{},
		ctl: newStreamController(), metrics: &streamMetrics{},
	}
	state.ctl.drop = 6
	frames := &frameStore{wake: make(chan struct{}, 1)}
	frames.put(Frame{Seq: 1, Image: noisyFrame(1, 128, 128), At: time.Now()})
	var out sentUpdates
	go func() { _ = writeUpdates(ctx, state, frames, out.write) }()
	for seq := uint64(2); seq < 8; seq++ {
		waitFor(t, func() bool { return out.count() >= int(seq-1) })
		frames.put(Frame{Seq: seq, Image: noisyFrame(int64(seq), 128, 128), At: time.Now()})
	}
	waitFor(t, func() bool { return out.count() >= 7 })
	if s := state.metrics.summary(); s.Refines != 0 {
		t.Fatalf("sharpened %d times while the screen moved", s.Refines)
	}
}

// The observer hears the stages of a fenced stream, and a summary at its end.
func TestObserverHearsStatsAndASummary(t *testing.T) {
	fake := NewFakeBackend(320, 200)
	var mu sync.Mutex
	var got []StreamStats
	var summary *StreamSummary
	obs := &Observer{
		Stats: func(s StreamStats) { mu.Lock(); got = append(got, s); mu.Unlock() },
		Done:  func(s StreamSummary) { mu.Lock(); summary = &s; mu.Unlock() },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, _ := fake.Start(ctx)
	viewerConn, server := netPipe()
	done := make(chan struct{})
	go func() { _ = serveRFB(ctx, server, session, "Test screen", obs); close(done) }()
	v := &testViewer{t: t, conn: viewerConn}
	v.write([]byte{1})
	head := v.read(24)
	v.read(int(binary.BigEndian.Uint32(head[20:])))
	v.fb = image.NewRGBA(image.Rect(0, 0, 320, 200))
	v.write(append([]byte{msgSetPixelFormat, 0, 0, 0}, novncFormat.bytes()...))
	v.setEncodings(encTight, encRaw, pseudoQualityLevel0+9, pseudoFence, pseudoContinuousUpdts)
	v.next() // continuous updates announced
	v.write([]byte{msgEnableContinuous, 1, 0, 0, 0, 0, 1, 64, 0, 200})
	deadline := time.Now().Add(3 * time.Second)
	for i := 0; ; i++ {
		msg := v.next()
		if msg.kind == msgFence {
			time.Sleep(20 * time.Millisecond) // the viewer takes a while to draw
			v.write(append([]byte{msgFence, 0, 0, 0, 0, 0, 0, 0, byte(len(msg.fence))}, msg.fence...))
			// Moving the pointer changes the screen, so another update follows.
			x := 10 + (i*7)%290
			v.write([]byte{msgPointerEvent, 0, byte(x >> 8), byte(x), 0, 100})
		}
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no stats within 3 s")
		}
	}
	mu.Lock()
	stats := got[0]
	mu.Unlock()
	if stats.Updates == 0 || stats.Bytes == 0 || stats.EncodeMs <= 0 || stats.AckMs < 20 || stats.Quality == 0 || stats.InFlight < inFlightBase {
		t.Fatalf("stats %+v", stats)
	}
	viewerConn.Close()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if summary == nil || summary.Updates == 0 || summary.AckP50 < 20 {
		t.Fatalf("summary %+v", summary)
	}
}
