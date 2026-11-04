//go:build darwin && !screenfake

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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stillResult struct {
	gapMs, fullMs         float64 // last motion to the first sharp update / to full sharp coverage
	coverage              float64 // share of the screen the sharp tiles repainted
	meanDiff, psnr        float64 // against a direct capture, over the repainted text crop
	edgeRatio             float64 // gradient energy of the viewer's crop over the direct capture's
	motionUpdates         int
	motionVideoUpdates    int
	pos0, pos1, posViewer int    // the page's scroll position: before, after (direct), in the viewer's sharp picture
	invalid               string // why the numbers cannot be trusted, if so
}

// TestRealScreenStillAndSharpness drives the real Mac screen (a Safari page
// scrolled by wheel events), stops, and records what a viewer receives: how
// long after the last moving update the sharp picture arrives, and how close
// the viewer's picture then is to a direct capture of the same screen, for
// tiles alone and for video. MEWLA_SCREEN_REAL_BENCH=1; the crops are saved
// under MEWLA_SCREEN_BENCH_OUT when it is set.
func TestRealScreenStillAndSharpness(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_REAL_BENCH") == "" {
		t.Skip("opens a Safari window and scrolls it: set MEWLA_SCREEN_REAL_BENCH=1")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := newBackend()
	if capability := backend.Capability(ctx); !capability.Ready() {
		t.Skipf("Screen is not ready here: %+v", capability)
	}
	out := os.Getenv("MEWLA_SCREEN_BENCH_OUT")

	var table strings.Builder
	fmt.Fprintf(&table, "\n%-8s %9s %9s %9s %9s %9s %8s %9s\n", "mode", "gap ms", "full ms", "cover %", "diff", "PSNR dB", "edge", "video %")
	for _, mode := range []struct {
		name  string
		video videoFactory
	}{{"tiles", nil}, {"video", newVTEncoder}} {
		// A fresh session per mode so one's leftovers do not reach the other.
		// Its own tab per mode: the page scrolls itself once, 3 s after it
		// opens, for 4 s, and stays still; no input of any kind.
		openScrollFixture(t, mode.name, 7000, 4000, false)
		time.Sleep(4 * time.Second)
		session, err := backend.Start(ctx)
		if err != nil {
			t.Fatal(err)
		}
		r := probeStill(t, ctx, session, mode.name, mode.video, out)
		session.Close()
		vp := 0
		if r.motionUpdates > 0 {
			vp = r.motionVideoUpdates * 100 / r.motionUpdates
		}
		if r.invalid != "" {
			fmt.Fprintf(&table, "%-8s INVALID: %s\n", mode.name, r.invalid)
			continue
		}
		fmt.Fprintf(&table, "%-8s %9.0f %9.0f %9.0f %9.2f %9.1f %8.2f %9d   scroll %d -> %d\n", mode.name, r.gapMs, r.fullMs, r.coverage*100, r.meanDiff, r.psnr, r.edgeRatio, vp, r.pos0, r.pos1)
	}
	t.Log(table.String())
}

func probeStill(t *testing.T, ctx context.Context, session Session, name string, video videoFactory, out string) stillResult {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var res stillResult
	before, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ok0 bool
	res.pos0, ok0 = readScrollMarker(before.Image)
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
				if pos, ok := readScrollMarker(run.img); ok {
					trace = append(trace, posAt{int(now.Sub(began).Milliseconds()), pos})
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
	pos1, ok1 := readScrollMarker(direct.Image)
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
	if res.pos1 == 0 || res.pos1 == res.pos0 {
		res.invalid = "the page did not scroll"
		return res
	}
	a, ok := analyzeStill(updates, screen.Dx()*screen.Dy())
	if !ok {
		res.invalid = fmt.Sprintf("no scroll then still found in %d updates (run %d..%d, gap %v)", len(updates), a.motionFirst, a.motionLast, a.gap)
		return res
	}
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
	canvas, painted, paintedN := still.img, still.painted, still.n
	if pos, ok := readScrollMarker(canvas); ok {
		res.posViewer = pos
	}
	if res.posViewer != res.pos1 {
		res.invalid = fmt.Sprintf("the viewer's still picture shows scroll %d, the direct capture %d", res.posViewer, res.pos1)
		return res
	}
	res.coverage = float64(paintedN) / float64(len(painted))
	// Text crop: the left-hand column of the page, where the paragraphs are.
	crop := image.Rect(screen.Min.X+40, screen.Min.Y+screen.Dy()/4, min(screen.Min.X+940, screen.Max.X), screen.Min.Y+screen.Dy()*3/4)
	var sq, sad float64
	var n int
	var gv, gd float64
	luma := func(img *image.RGBA, x, y int) float64 {
		c := img.RGBAAt(x, y)
		return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
	}
	for y := crop.Min.Y; y < crop.Max.Y-1; y++ {
		for x := crop.Min.X; x < crop.Max.X-1; x++ {
			if !painted[(y-screen.Min.Y)*screen.Dx()+x-screen.Min.X] {
				continue
			}
			a, d := luma(canvas, x, y), luma(direct.Image, x, y)
			sq += (a - d) * (a - d)
			sad += math.Abs(a - d)
			n++
			gv += math.Hypot(luma(canvas, x+1, y)-a, luma(canvas, x, y+1)-a)
			gd += math.Hypot(luma(direct.Image, x+1, y)-d, luma(direct.Image, x, y+1)-d)
		}
	}
	if n > 0 {
		res.meanDiff = sad / float64(n)
		if sq > 0 {
			res.psnr = 10 * math.Log10(255*255/(sq/float64(n)))
		} else {
			res.psnr = 99
		}
		if gd > 0 {
			res.edgeRatio = gv / gd
		}
	}
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

// openScrollFixture opens, in Safari, a long page that scrolls itself with
// requestAnimationFrame: no wheel, key or click events reach the Mac. The
// settings are written into the file (Safari drops a file URL's query): delay
// (ms after load before it starts), run (ms of scrolling, then it stays
// still) or loop (scroll up and down for ever). Its title carries the label.
// A marker in the top left corner shows the scroll position in binary (see
// readScrollMarker), so any captured frame says where the page was.
func openScrollFixture(t *testing.T, label string, delay, run int, loop bool) {
	t.Helper()
	page := filepath.Join(t.TempDir(), "mewla-scroll-fixture-"+label+".html")
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
const delay = DELAY, run = RUN, loop = LOOP, speed = 900;
const bar = document.createElement('div');
bar.style.cssText = 'position:fixed;left:0;top:0;display:flex;z-index:99999';
const cells = [];
for (let i = 0; i < 17; i++) { const d = document.createElement('div'); d.style.cssText = 'width:20px;height:20px'; bar.append(d); cells.push(d); }
document.body.append(bar);
const mark = () => {
  const y = Math.round(scrollY) & 0xffff;
  cells[0].style.background = '#0f0';
  for (let i = 0; i < 16; i++) cells[i + 1].style.background = (y >> (15 - i)) & 1 ? '#f00' : '#00f';
};
mark();
setTimeout(() => {
  const t0 = performance.now();
  const tick = (now) => {
    const el = now - t0;
    if (!loop && el >= run) { mark(); return; }
    const max = document.documentElement.scrollHeight - innerHeight;
    let y = speed * el / 1000;
    if (loop) { const p = y % (2 * max); y = p < max ? p : 2 * max - p; }
    scrollTo(0, y);
    mark();
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}, delay);
</script>`)
	body := strings.NewReplacer("DELAY", fmt.Sprint(delay), "RUN", fmt.Sprint(run), "LOOP", fmt.Sprint(loop), "LABEL", label).Replace(html.String())
	if err := os.WriteFile(page, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	u := url.URL{Scheme: "file", Path: page}
	if err := exec.Command("open", "-a", "Safari", u.String()).Run(); err != nil {
		t.Skipf("no Safari: %v", err)
	}
}
