package screen

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The still-frame probe, independent of the platform: it takes any Session
// whose screen shows the fixture page's marker, so it runs against the real
// Safari page on a Mac and against a synthetic page anywhere.

type stillResult struct {
	gapMs, fullMs         float64 // last motion to the first sharp update / to full sharp coverage
	coverage              float64 // share of the screen the sharp tiles repainted
	meanDiff, psnr        float64 // against a direct capture, over the repainted text crop
	edgeRatio             float64 // gradient energy of the viewer's crop over the direct capture's
	motionUpdates         int
	motionVideoUpdates    int
	pos0, pos1, posViewer int    // the page's scroll position: before, after (direct), in the viewer's sharp picture
	invalid               string // why the numbers cannot be trusted, if so
	noSharpen             bool   // tiles were already at the viewer's quality: no sharpening update followed
}

func probeStill(t *testing.T, ctx context.Context, session Session, name string, video videoFactory, out string) stillResult {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var res stillResult
	// The page must be drawing before anything is measured: its marker is on
	// the screen and its frame counter advances.
	before, m0, err := awaitFixture(ctx, session, 12*time.Second)
	if err != nil {
		res.invalid = err.Error()
		return res
	}
	res.pos0 = m0.Pos
	ok0 := true
	conn, server := shapedPipe(linkShape{"LAN", 2 * time.Millisecond, 100e6 / 8, false})
	was := streamAdaptive
	streamAdaptive = true
	defer func() { streamAdaptive = was }()
	done := make(chan struct{})
	go func() { _ = serveRFBVideo(ctx, server, session, "Fixture", nil, video); close(done) }()
	say := func(p []byte) {
		if _, err := conn.Write(p); err != nil {
			t.Fatal(err)
		}
	}
	say([]byte{1})
	head := make([]byte, 24)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatal(err)
	}
	serverW, serverH := int(binary.BigEndian.Uint16(head[0:])), int(binary.BigEndian.Uint16(head[2:]))
	if _, err := io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint32(head[20:]))); err != nil {
		t.Fatal(err)
	}
	say(append([]byte{msgSetPixelFormat, 0, 0, 0}, novncFormat.bytes()...))
	encodings := []int32{encTight, encRaw, pseudoQualityLevel0 + 6, pseudoDesktopSize, pseudoFence, pseudoContinuousUpdts}
	if video != nil {
		encodings = append([]int32{encH264}, encodings...)
	}
	set := []byte{msgSetEncodings, 0, 0, byte(len(encodings))}
	for _, e := range encodings {
		set = binary.BigEndian.AppendUint32(set, uint32(e))
	}
	say(set)

	var cur []stillRect
	skim := &skimmer{conn: conn, onRect: func(r image.Rectangle, enc int32, body, fill []byte) {
		cur = append(cur, stillRect{r: r, video: enc == encH264, jpeg: body, fill: fill})
	}}
	skim.message()
	say([]byte{msgEnableContinuous, 1, 0, 0, 0, 0, 5, 0, 3, 32})

	screen := before.Image.Bounds()
	run := newStillCanvas(screen) // every picture the viewer got, painted as it came
	var updates []stillUpdate
	type posAt struct{ ms, pos int }
	var trace []posAt
	sawStall := false
	began := time.Now()
	end := began.Add(14 * time.Second)
	_ = conn.SetReadDeadline(end) // the skimmer panics on a read error: a quiet stream must not run past 14 s
	for time.Now().Before(end) && !func() (stop bool) {
		defer func() {
			if recover() != nil {
				stop = true
			}
		}()
		cur = nil
		kind, fence, _ := skim.message()
		switch kind {
		case msgFence:
			time.Sleep(8 * time.Millisecond)
			say(append([]byte{msgFence, 0, 0, 0, 0, 0, 0, 0, byte(len(fence))}, fence...))
		case msgFramebufferUpdate:
			now := time.Now()
			updates = append(updates, stillUpdate{now, cur})
			if run.paint(cur) {
				if m, ok := readMarker(run.img); ok {
					trace = append(trace, posAt{int(now.Sub(began).Milliseconds()), m.Pos})
					if m.Stalled {
						sawStall = true
					}
				}
			}
		}
		return false
	}() {
	}
	conn.Close()
	<-done
	// The page's scroll has ended long before: a direct capture of the same
	// still screen to compare with.
	direct, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	m1, ok1 := readMarker(direct.Image)
	pos1 := m1.Pos
	res.pos1 = pos1
	// Every change of position the viewer's tile pictures showed, and when
	// (a position seen twice in a row is not repeated).
	var line strings.Builder
	last := -1
	for _, p := range trace {
		if p.pos != last {
			fmt.Fprintf(&line, " %dms:%d", p.ms, p.pos)
			last = p.pos
		}
	}
	t.Logf("%s: %d updates in %v; direct scroll marker before %d (read ok: %v), after %d (read ok: %v), frame %v; tile pictures' position changes:%s",
		name, len(updates), time.Since(began).Round(time.Millisecond), res.pos0, ok0, pos1, ok1, direct.Image.Bounds(), line.String())
	if out != "" {
		var buf bytes.Buffer
		if png.Encode(&buf, direct.Image.SubImage(image.Rect(0, 0, min(600, direct.Image.Bounds().Dx()), 120))) == nil {
			_ = os.WriteFile(filepath.Join(out, "marker-direct-"+name+".png"), buf.Bytes(), 0o600)
		}
	}
	if db := direct.Image.Bounds(); db.Dx() != serverW || db.Dy() != serverH {
		res.invalid = fmt.Sprintf("the stream is %dx%d but a direct capture is %dx%d", serverW, serverH, db.Dx(), db.Dy())
		return res
	}
	if !ok1 || m1.Phase != phaseDone || m1.Pos == 0 || m1.Pos == res.pos0 {
		res.invalid = fmt.Sprintf("the page did not finish a scroll (direct capture shows %v, read ok %v)", m1, ok1)
		return res
	}
	if m1.Stalled || sawStall {
		res.invalid = "the page stalled or was hidden while it scrolled"
		return res
	}
	a, ok := analyzeStill(updates, screen.Dx()*screen.Dy())
	var canvas *image.RGBA
	var painted []bool
	var paintedN int
	switch {
	case ok:
		res.motionUpdates = a.motionLast - a.motionFirst + 1
		res.motionVideoUpdates = a.motionVideo
		lastMotion := updates[a.motionLast].at
		res.gapMs = float64(a.gap.Milliseconds())
		still := newStillCanvas(screen) // only what came after the quiet gap
		for _, u := range updates[a.stillIdx:] {
			still.paint(u.rects)
			if res.fullMs == 0 && still.share() >= 0.95 {
				res.fullMs = float64(u.at.Sub(lastMotion).Milliseconds())
			}
		}
		canvas, painted, paintedN = still.img, still.painted, still.n
	case a.noStill && video == nil:
		// Tiles that went out at the viewer's own quality need no second
		// look: the picture the viewer built while the page moved is the
		// sharp one.
		res.motionUpdates = a.motionLast - a.motionFirst + 1
		res.noSharpen = true
		canvas, painted, paintedN = run.img, run.painted, run.n
	default:
		res.invalid = fmt.Sprintf("no scroll then still found in %d updates (run %d..%d, gap %v, nothing followed %v)", len(updates), a.motionFirst, a.motionLast, a.gap, a.noStill)
		return res
	}
	if m, ok := readMarker(canvas); ok {
		res.posViewer = m.Pos
	}
	if res.posViewer != res.pos1 {
		res.invalid = fmt.Sprintf("the viewer's final picture shows scroll %d, the direct capture %d", res.posViewer, res.pos1)
		return res
	}
	res.coverage = float64(paintedN) / float64(len(painted))
	crop := textCrop(screen)
	res.meanDiff, res.psnr, res.edgeRatio = textMetrics(canvas, direct.Image, painted, crop)
	if out != "" {
		save := func(file string, img *image.RGBA) {
			var buf bytes.Buffer
			if png.Encode(&buf, img.SubImage(crop)) == nil {
				_ = os.WriteFile(filepath.Join(out, file), buf.Bytes(), 0o600)
			}
		}
		save("crop-viewer-"+name+".png", canvas)
		save("crop-direct-"+name+".png", direct.Image)
	}
	return res
}

// awaitFixture waits until the fixture page is on the screen and drawing: its
// marker is found, it is waiting to scroll, and its frame counter advances
// between two captures. It returns the second capture.
func awaitFixture(ctx context.Context, session Session, within time.Duration) (Frame, pageMarker, error) {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	var seq uint64
	var first *pageMarker
	for {
		f, err := session.Frame(ctx, seq)
		if err != nil {
			if first == nil {
				return Frame{}, pageMarker{}, fmt.Errorf("the fixture page's marker never appeared on the screen (hidden, occluded or not loaded): %v", err)
			}
			return Frame{}, pageMarker{}, fmt.Errorf("the fixture page stopped drawing (hidden or occluded?): its frame counter stayed at %d", first.Beat)
		}
		seq = f.Seq
		m, ok := readMarker(f.Image)
		if !ok {
			continue
		}
		if m.Phase != phaseWaiting || m.Stalled {
			return Frame{}, pageMarker{}, fmt.Errorf("the fixture page was not waiting to scroll when the probe began: %v", m)
		}
		if first == nil {
			first = &m
			continue
		}
		if m.Beat != first.Beat {
			return f, m, nil
		}
	}
}

// stillCanvas is the picture a viewer builds from tight rectangles.
type stillCanvas struct {
	img     *image.RGBA
	painted []bool
	n       int
}

func newStillCanvas(r image.Rectangle) *stillCanvas {
	return &stillCanvas{img: image.NewRGBA(r), painted: make([]bool, r.Dx()*r.Dy())}
}

func (c *stillCanvas) share() float64 { return float64(c.n) / float64(len(c.painted)) }

// paint draws the tile rectangles (video is not decoded here) and reports
// whether any touched the top rows, where the scroll marker is.
func (c *stillCanvas) paint(rects []stillRect) (top bool) {
	b := c.img.Bounds()
	for _, r := range rects {
		var img image.Image
		switch {
		case r.video:
			continue
		case r.jpeg != nil:
			dec, err := jpeg.Decode(bytes.NewReader(r.jpeg))
			if err != nil {
				continue
			}
			img = dec
		case len(r.fill) == 3:
			img = image.NewUniform(color.RGBA{r.fill[0], r.fill[1], r.fill[2], 255})
		default:
			continue
		}
		if r.r.Min.Y < b.Min.Y+400 {
			top = true
		}
		for y := r.r.Min.Y; y < r.r.Max.Y && y < b.Max.Y; y++ {
			for x := r.r.Min.X; x < r.r.Max.X && x < b.Max.X; x++ {
				if u, ok := img.(*image.Uniform); ok {
					c.img.Set(x, y, u.C)
				} else {
					c.img.Set(x, y, img.At(img.Bounds().Min.X+x-r.r.Min.X, img.Bounds().Min.Y+y-r.r.Min.Y))
				}
				i := (y-b.Min.Y)*b.Dx() + x - b.Min.X
				if !c.painted[i] {
					c.painted[i] = true
					c.n++
				}
			}
		}
	}
	return top
}

// scrollFixtureHTML is a long page that scrolls itself with
// requestAnimationFrame, so no wheel, key or click event ever reaches the
// computer. Settings are written into the file (Safari drops a file URL's
// query): delay is the time of continuous drawing before it scrolls, run the
// milliseconds of scrolling after which it stays still (phase done, counter
// frozen), loop scrolls up and down for ever. The two-row marker in its top
// left corner (see readMarker) lets any capture prove that the page was
// visible and drawing: a hidden or stalled page latches the stall bit for the
// rest of the scroll.
func scrollFixtureHTML(label string, delay, run int, loop bool) string {
	var html strings.Builder
	html.WriteString("<!doctype html><title>mewla-scroll-fixture</title><style>html{scroll-behavior:auto}</style><body style='font:16px Helvetica;margin:40px'>")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&html, "<p style='max-width:900px'>Paragraph %d. The quick brown fox jumps over the lazy dog while a page of ordinary text scrolls past the capture, with a picture every so often.</p>", i)
		if i%6 == 0 {
			fmt.Fprintf(&html, "<div style='height:240px;max-width:900px;background:linear-gradient(%ddeg,hsl(%d,70%%,60%%),hsl(%d,70%%,35%%))'></div>", i*7%360, i*37%360, (i*37+120)%360)
		}
	}
	html.WriteString(`<script>
document.title = 'mewla-scroll-fixture LABEL';
const delay = DELAY, run = RUN, loop = LOOP, speed = 900, maxGap = 250;
const bar = document.createElement('div');
bar.style.cssText = 'position:fixed;left:0;top:0;z-index:99999';
const rows = [];
for (let r = 0; r < 2; r++) {
  const row = document.createElement('div');
  row.style.cssText = 'display:flex';
  const cells = [];
  for (let i = 0; i < 17; i++) { const d = document.createElement('div'); d.style.cssText = 'width:20px;height:20px'; row.append(d); cells.push(d); }
  bar.append(row); rows.push(cells);
}
document.body.append(bar);
let phase = 0, stalled = false, beat = 0, last = 0, visibleSince = 0, t0 = 0;
const bits = (cells, v) => { for (let i = 0; i < 16; i++) cells[i + 1].style.background = (v >> (15 - i)) & 1 ? '#f00' : '#00f'; };
const draw = () => {
  rows[0][0].style.background = '#0f0';
  rows[1][0].style.background = '#ff0';
  bits(rows[0], Math.round(scrollY) & 0xffff);
  bits(rows[1], (phase << 14) | (stalled ? 1 << 13 : 0) | (beat & 0x1fff));
};
document.addEventListener('visibilitychange', () => { if (document.hidden && phase === 1) stalled = true; if (document.hidden) visibleSince = 0; });
const frame = (now) => {
  if (phase === 2) return;
  const gap = last ? now - last : 0;
  last = now;
  beat++;
  if (document.hidden || gap > maxGap) {
    if (phase === 1) stalled = true;
    visibleSince = 0;
  }
  if (phase === 0) {
    if (!visibleSince) visibleSince = now;
    if (now - visibleSince >= delay) { phase = 1; t0 = now; }
  }
  if (phase === 1) {
    const el = now - t0;
    if (!loop && el >= run) { phase = 2; scrollTo(0, speed * run / 1000); draw(); return; }
    const max = document.documentElement.scrollHeight - innerHeight;
    let y = speed * el / 1000;
    if (loop) { const p = y % (2 * max); y = p < max ? p : 2 * max - p; }
    scrollTo(0, y);
  }
  draw();
  requestAnimationFrame(frame);
};
draw();
requestAnimationFrame(frame);
</script>`)
	return strings.NewReplacer("DELAY", fmt.Sprint(delay), "RUN", fmt.Sprint(run), "LOOP", fmt.Sprint(loop), "LABEL", label).Replace(html.String())
}

func readPNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewRGBA(dec.Bounds())
	for y := out.Rect.Min.Y; y < out.Rect.Max.Y; y++ {
		for x := out.Rect.Min.X; x < out.Rect.Max.X; x++ {
			out.Set(x, y, dec.At(x, y))
		}
	}
	return out
}

// textCrop is the left-hand column of the page, where the paragraphs are.
func textCrop(screen image.Rectangle) image.Rectangle {
	return image.Rect(screen.Min.X+40, screen.Min.Y+screen.Dy()/4, min(screen.Min.X+940, screen.Max.X), screen.Min.Y+screen.Dy()*3/4)
}

// textMetrics compares a viewer's picture with a direct capture over crop,
// on the pixels the viewer painted (all, when painted is nil): mean absolute
// luma difference, PSNR, and the ratio of gradient energy (sharpness).
func textMetrics(viewer, direct *image.RGBA, painted []bool, crop image.Rectangle) (diff, psnr, edge float64) {
	b := direct.Bounds()
	luma := func(img *image.RGBA, x, y int) float64 {
		c := img.RGBAAt(x, y)
		return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
	}
	var sq, sad, gv, gd float64
	var n int
	for y := crop.Min.Y; y < crop.Max.Y-1; y++ {
		for x := crop.Min.X; x < crop.Max.X-1; x++ {
			if painted != nil && !painted[(y-b.Min.Y)*b.Dx()+x-b.Min.X] {
				continue
			}
			a, d := luma(viewer, x, y), luma(direct, x, y)
			sq += (a - d) * (a - d)
			sad += math.Abs(a - d)
			n++
			gv += math.Hypot(luma(viewer, x+1, y)-a, luma(viewer, x, y+1)-a)
			gd += math.Hypot(luma(direct, x+1, y)-d, luma(direct, x, y+1)-d)
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	diff = sad / float64(n)
	psnr = 99.0
	if sq > 0 {
		psnr = 10 * math.Log10(255*255/(sq/float64(n)))
	}
	if gd > 0 {
		edge = gv / gd
	}
	return diff, psnr, edge
}
