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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type stillResult struct {
	gapMs, fullMs      float64 // last motion to the first sharp update / to full sharp coverage
	coverage           float64 // share of the screen the sharp tiles repainted
	meanDiff, psnr     float64 // against a direct capture, over the repainted text crop
	edgeRatio          float64 // gradient energy of the viewer's crop over the direct capture's
	motionUpdates      int
	motionVideoUpdates int
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
	page := filepath.Join(t.TempDir(), "mewla-scroll-fixture.html")
	var html strings.Builder
	html.WriteString("<!doctype html><title>mewla-scroll-fixture</title><body style='font:16px Helvetica;margin:40px'>")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&html, "<p style='max-width:900px'>Paragraph %d. The quick brown fox jumps over the lazy dog while a page of ordinary text scrolls past the capture, with a picture every so often.</p>", i)
		if i%6 == 0 {
			fmt.Fprintf(&html, "<div style='height:240px;max-width:900px;background:linear-gradient(%ddeg,hsl(%d,70%%,60%%),hsl(%d,70%%,35%%))'></div>", i*7%360, i*37%360, (i*37+120)%360)
		}
	}
	if err := os.WriteFile(page, []byte(html.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("open", "-a", "Safari", page).Run(); err != nil {
		t.Skipf("no Safari: %v", err)
	}
	time.Sleep(4 * time.Second)
	out := os.Getenv("MEWLA_SCREEN_BENCH_OUT")

	var table strings.Builder
	fmt.Fprintf(&table, "\n%-8s %9s %9s %9s %9s %9s %8s %9s\n", "mode", "gap ms", "full ms", "cover %", "diff", "PSNR dB", "edge", "video %")
	for _, mode := range []struct {
		name  string
		video videoFactory
	}{{"tiles", nil}, {"video", newVTEncoder}} {
		// A fresh session per mode so one's leftovers do not reach the other.
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
		fmt.Fprintf(&table, "%-8s %9.0f %9.0f %9.0f %9.2f %9.1f %8.2f %9d\n", mode.name, r.gapMs, r.fullMs, r.coverage*100, r.meanDiff, r.psnr, r.edgeRatio, vp)
	}
	t.Log(table.String())
}

func probeStill(t *testing.T, ctx context.Context, session Session, name string, video videoFactory, out string) stillResult {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	first, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	b := first.Image.Bounds()
	cx, cy := b.Dx()/2, b.Dy()/2
	var moving atomic.Bool
	moving.Store(true)
	go func() {
		for step := 0; ctx.Err() == nil; step++ {
			time.Sleep(time.Second / 30)
			if moving.Load() && step%2 == 0 {
				_ = session.Pointer(cx, cy, 16)
				_ = session.Pointer(cx, cy, 0)
			}
		}
	}()
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

	type got struct {
		at    time.Time
		rects []stillRect
	}
	var cur []stillRect
	skim := &skimmer{conn: conn, onRect: func(r image.Rectangle, enc int32, body, fill []byte) {
		cur = append(cur, stillRect{r: r, video: enc == encH264, jpeg: body, fill: fill})
	}}
	skim.message()
	say([]byte{msgEnableContinuous, 1, 0, 0, 0, 0, 5, 0, 3, 32})

	var updates []got
	began := time.Now()
	stopAt := began.Add(5 * time.Second)
	end := stopAt.Add(3 * time.Second)
	for time.Now().Before(end) {
		if moving.Load() && time.Now().After(stopAt) {
			moving.Store(false)
		}
		cur = nil
		kind, fence, _ := skim.message()
		switch kind {
		case msgFence:
			time.Sleep(8 * time.Millisecond)
			say(append([]byte{msgFence, 0, 0, 0, 0, 0, 0, 0, byte(len(fence))}, fence...))
		case msgFramebufferUpdate:
			updates = append(updates, got{time.Now(), cur})
		}
	}
	conn.Close()
	<-done
	// The screen is still now: a direct capture to compare with.
	direct, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Last moving update: the one before the first quiet gap of 200 ms
	// after the warm-up.
	var res stillResult
	stillIdx := -1
	for i := 1; i < len(updates); i++ {
		if updates[i].at.Sub(began) > 2500*time.Millisecond && updates[i].at.Sub(updates[i-1].at) >= 200*time.Millisecond {
			stillIdx = i
			break
		}
	}
	if stillIdx < 0 {
		t.Logf("%s: no still update seen (%d updates)", name, len(updates))
		return res
	}
	for _, u := range updates[:stillIdx] {
		if u.at.Sub(began) > 2500*time.Millisecond {
			res.motionUpdates++
			for _, r := range u.rects {
				if r.video {
					res.motionVideoUpdates++
					break
				}
			}
		}
	}
	lastMotion := updates[stillIdx-1].at
	res.gapMs = float64(updates[stillIdx].at.Sub(lastMotion).Milliseconds())
	screen := direct.Image.Bounds()
	canvas := image.NewRGBA(screen)
	painted := make([]bool, screen.Dx()*screen.Dy())
	paintedN := 0
	for _, u := range updates[stillIdx:] {
		for _, r := range u.rects {
			if r.video {
				continue
			}
			var img image.Image
			if r.jpeg != nil {
				if img, err = jpeg.Decode(bytes.NewReader(r.jpeg)); err != nil {
					continue
				}
			} else if len(r.fill) == 3 {
				img = image.NewUniform(color.RGBA{r.fill[0], r.fill[1], r.fill[2], 255})
			} else {
				continue
			}
			for y := r.r.Min.Y; y < r.r.Max.Y && y < screen.Max.Y; y++ {
				for x := r.r.Min.X; x < r.r.Max.X && x < screen.Max.X; x++ {
					if u, ok := img.(*image.Uniform); ok {
						canvas.Set(x, y, u.C)
					} else {
						canvas.Set(x, y, img.At(img.Bounds().Min.X+x-r.r.Min.X, img.Bounds().Min.Y+y-r.r.Min.Y))
					}
					i := (y-screen.Min.Y)*screen.Dx() + x - screen.Min.X
					if !painted[i] {
						painted[i] = true
						paintedN++
					}
				}
			}
		}
		if res.fullMs == 0 && float64(paintedN) >= 0.95*float64(len(painted)) {
			res.fullMs = float64(u.at.Sub(lastMotion).Milliseconds())
		}
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

type stillRect struct {
	r     image.Rectangle
	video bool
	jpeg  []byte
	fill  []byte
}
