package screen

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// markerSession is a screen that behaves like the fixture page: it shows its
// marker, waits (drawing a frame counter at 30 Hz), scrolls a page of
// text-like lines at 900 px/s, and stops. It lets the still-frame probe and
// the viewer end-to-end test run anywhere.
type markerSession struct {
	start       time.Time
	w, h        int
	wait, run   time.Duration
	hiddenAfter time.Duration // when non-zero, the page stops drawing from this time (a hidden tab)
	img         *image.RGBA
}

func newMarkerSession(w, h int, wait, run time.Duration) *markerSession {
	return &markerSession{start: time.Now(), w: w, h: h, wait: wait, run: run}
}

const markerTick = time.Second / 30

// state is the page at a moment: its tick number, which is the sequence
// number of what the screen shows, and the marker.
func (s *markerSession) state(at time.Time) (seq uint64, m pageMarker) {
	el := at.Sub(s.start)
	if s.hiddenAfter > 0 && el > s.hiddenAfter {
		el = s.hiddenAfter
	}
	switch {
	case el >= s.wait+s.run:
		end := int((s.wait + s.run) / markerTick)
		return uint64(end + 2), pageMarker{Pos: int(900 * s.run.Seconds()), Phase: phaseDone, Beat: end & 0x1fff}
	case el >= s.wait:
		n := int(el / markerTick)
		return uint64(n + 1), pageMarker{Pos: int(900 * (el - s.wait).Seconds()), Phase: phaseScrolling, Beat: n & 0x1fff}
	}
	n := int(el / markerTick)
	return uint64(n + 1), pageMarker{Phase: phaseWaiting, Beat: n & 0x1fff}
}

func (s *markerSession) Frame(ctx context.Context, after uint64) (Frame, error) {
	for {
		now := time.Now()
		seq, m := s.state(now)
		if seq > after {
			if s.img == nil {
				s.img = image.NewRGBA(image.Rect(0, 0, s.w, s.h))
			}
			s.draw(m)
			return Frame{Seq: seq, Image: s.img, At: now}, nil
		}
		// Nothing new: a page that is done, or hidden, never changes again.
		var next time.Duration = markerTick
		if m.Phase == phaseDone || (s.hiddenAfter > 0 && now.Sub(s.start) > s.hiddenAfter) {
			next = time.Hour
		}
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-time.After(next):
		}
	}
}

// draw paints text-like lines (grey runs of hashed widths on white, 24 px
// pitch, 14 px tall) shifted by the scroll position, then the marker.
func (s *markerSession) draw(m pageMarker) {
	img := s.img
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	ink := color.RGBA{40, 40, 40, 255}
	for line := 0; line*24-m.Pos < s.h; line++ {
		top := 60 + line*24 - m.Pos
		if top+14 < 0 || top >= s.h {
			continue
		}
		h := fnv.New32a()
		h.Write([]byte{byte(line), byte(line >> 8)})
		seed := h.Sum32()
		x := 40
		for x < s.w-80 {
			wordLen := 20 + int(seed%60)
			seed = seed*1664525 + 1013904223
			for xx := x; xx < x+wordLen && xx < s.w-40; xx++ {
				for yy := max(top, 0); yy < min(top+14, s.h); yy++ {
					// A glyph-like pattern: vertical strokes with gaps.
					if (xx/3+yy/4+int(seed>>8))%3 != 0 {
						img.SetRGBA(xx, yy, ink)
					}
				}
			}
			x += wordLen + 10
		}
	}
	drawMarker(img, 0, 0, 20, m)
}

func (s *markerSession) Pointer(int, int, uint8) error { return nil }
func (s *markerSession) Key(uint32, bool) error        { return nil }
func (s *markerSession) Close() error                  { return nil }

// The whole probe, on a synthetic page: tiles and video.
func TestStillProbeOnASyntheticPage(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_LINK_BENCH") == "" {
		t.Skip("takes 15 s per mode: set MEWLA_SCREEN_LINK_BENCH=1")
	}
	if !ffmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	for _, mode := range []struct {
		name  string
		video videoFactory
	}{{"tiles", nil}, {"video", newFFmpegEncoder}} {
		session := newMarkerSession(1280, 800, 2*time.Second, 3*time.Second)
		r := probeStill(t, context.Background(), session, mode.name, mode.video, "")
		if r.invalid != "" {
			t.Fatalf("%s: %s", mode.name, r.invalid)
		}
		t.Logf("%s: %+v", mode.name, r)
		if mode.video == nil {
			if !r.noSharpen {
				t.Errorf("tiles on a fast link were sharpened again (gap %v ms)", r.gapMs)
			}
		} else if r.gapMs < 240 || r.gapMs > 700 || r.fullMs == 0 {
			t.Errorf("%s: still frame %v ms after the last motion, full at %v", mode.name, r.gapMs, r.fullMs)
		}
		if r.coverage < 0.9 {
			t.Errorf("%s: sharp picture covers %.0f%%", mode.name, r.coverage*100)
		}
		if r.psnr < 25 || r.edgeRatio < 0.7 {
			t.Errorf("%s: PSNR %.1f edge %.2f", mode.name, r.psnr, r.edgeRatio)
		}
		if mode.video != nil && r.motionVideoUpdates*2 < r.motionUpdates {
			t.Errorf("video mode but %d of %d moving updates were video", r.motionVideoUpdates, r.motionUpdates)
		}
	}
}

// A page that stops drawing (a hidden tab) is invalid, not a pass.
func TestStillProbeRejectsAHiddenPage(t *testing.T) {
	session := newMarkerSession(640, 400, time.Hour, time.Second) // never scrolls
	session.hiddenAfter = 300 * time.Millisecond
	r := probeStill(t, context.Background(), session, "hidden", nil, "")
	if r.invalid == "" {
		t.Fatalf("a hidden page was measured: %+v", r)
	}
	t.Log(r.invalid)
}

// The fixture page's own logic, run under node with a stubbed DOM and a
// scripted clock: it scrolls after its wait, ends in phase done with the
// final position and frozen, resets its wait when hidden while waiting, and
// latches the stall bit when it is hidden or stops drawing mid-scroll.
func TestScrollFixtureScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	page := scrollFixtureHTML("t", 500, 1000, false)
	script := page[strings.Index(page, "<script>")+len("<script>") : strings.LastIndex(page, "</script>")]
	harness := `
const run = (steps) => {
  const cells = [];
  const mk = () => ({ style: {}, append(...c) { this.kids = (this.kids || []).concat(c); } });
  let scrollY = 0, hidden = false, raf = null, vis = null;
  const document = {
    title: '', get hidden() { return hidden; },
    createElement() { const e = mk(); cells.push(e); return e; },
    body: { append() {} },
    documentElement: { scrollHeight: 30000 },
    addEventListener(n, f) { if (n === 'visibilitychange') vis = f; },
  };
  const scrollTo = (x, y) => { scrollY = y; };
  const requestAnimationFrame = (f) => { raf = f; };
  const innerHeight = 800;
  // scrollY is read through a getter on globalThis for the page script.
  Object.defineProperty(globalThis, 'scrollY', { get: () => scrollY, configurable: true });
  new Function('document', 'scrollTo', 'requestAnimationFrame', 'innerHeight', SCRIPT)(document, scrollTo, requestAnimationFrame, innerHeight);
  const read = () => {
    const rows = [cells.slice(2, 19), cells.slice(20, 37)];
    // Rows are created in order: bar, row0, 17 cells, row1, 17 cells.
    const bits = (r) => { let v = 0; for (let i = 1; i < 17; i++) v = v * 2 + (r[i].style.background === '#f00' ? 1 : 0); return v; };
    const v2 = bits(rows[1]);
    return { pos: bits(rows[0]), phase: v2 >> 14, stalled: (v2 >> 13) & 1, beat: v2 & 0x1fff };
  };
  let now = 0;
  for (const step of steps) {
    if (step.hide !== undefined) { hidden = step.hide; if (vis) vis(); continue; }
    now += step.dt;
    const f = raf; raf = null;
    if (f) f(now);
  }
  return read();
};
const frames = (n, dt = 16.7) => Array.from({ length: n }, () => ({ dt }));
const out = {
  normal: run(frames(400)),
  gap: run([...frames(60), { dt: 400 }, ...frames(300)]),
  hiddenWaiting: run([...frames(10), { hide: true }, { hide: false }, ...frames(400)]),
  hiddenScrolling: run([...frames(60), { hide: true }, { hide: false }, ...frames(300)]),
};
console.log(JSON.stringify(out));
`
	harness = strings.Replace(harness, "SCRIPT", strconv.Quote(script), -1)
	dir := t.TempDir()
	file := filepath.Join(dir, "h.js")
	if err := os.WriteFile(file, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	outBytes, err := exec.Command(node, file).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, outBytes)
	}
	var got map[string]struct{ Pos, Phase, Stalled, Beat int }
	if err := json.Unmarshal(outBytes, &got); err != nil {
		t.Fatalf("%v: %s", err, outBytes)
	}
	t.Logf("%+v", got)
	if n := got["normal"]; n.Phase != phaseDone || n.Stalled != 0 || n.Pos != 900 {
		t.Errorf("normal: %+v", n)
	}
	if n := got["gap"]; n.Phase != phaseDone || n.Stalled != 1 {
		t.Errorf("a 400 ms stall while scrolling: %+v", n)
	}
	if n := got["hiddenWaiting"]; n.Phase != phaseDone || n.Stalled != 0 {
		t.Errorf("hidden while waiting should only restart the wait: %+v", n)
	}
	if n := got["hiddenScrolling"]; n.Phase != phaseDone || n.Stalled != 1 {
		t.Errorf("hidden while scrolling: %+v", n)
	}
}
