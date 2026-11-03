//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealScreenVideoOverSimulatedLinks measures the real thing: this Mac's
// own capture, a long page scrolled in Safari by scripted wheel events, the
// real hardware encoder, over the same simulated links as the fake-screen
// bench. It opens one Safari window (titled mewla-scroll-fixture, left open) and scrolls it for about two minutes; run
// it when nobody is using this Mac. MEWLA_SCREEN_REAL_BENCH=1 and -v.
func TestRealScreenVideoOverSimulatedLinks(t *testing.T) {
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
	// The window is left open (no AppleScript: closing it that way needs the
	// Automation permission, which waits for a person at the Mac). Its title is
	// mewla-scroll-fixture; the person running this closes it.
	time.Sleep(4 * time.Second)

	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	b := first.Image.Bounds()
	cx, cy := b.Dx()/2, b.Dy()/2
	drive := func(step int) {
		// One wheel notch every other tick; the page keeps moving.
		if step%2 == 0 {
			_ = session.Pointer(cx, cy, 16)
			_ = session.Pointer(cx, cy, 0)
		}
	}

	shapes := []linkShape{
		{"LAN 100 Mb/s, 2 ms", 2 * time.Millisecond, 100e6 / 8, false},
		{"relay 20 Mb/s, 150 ms RTT", 75 * time.Millisecond, 20e6 / 8, false},
		{"mobile 4 Mb/s, 80 ms RTT", 40 * time.Millisecond, 4e6 / 8, false},
		{"relay 5 Mb/s, 650 ms RTT", 325 * time.Millisecond, 5e6 / 8, false},
	}
	var table strings.Builder
	fmt.Fprintf(&table, "\n%-28s %-52s %-60s\n", "link (Safari page scrolling)", "tiles (adaptive)", "video (VideoToolbox)")
	for _, shape := range shapes {
		tiles := measureSession(t, ctx, session, drive, shape, true, nil)
		video := measureSession(t, ctx, session, drive, shape, true, newVTEncoder)
		fmt.Fprintf(&table, "%-28s %5.1f fps %6.0f kb/s ack %4.0f ms enc %4.1f ms cpu %3.0f%%   %5.1f fps %6.0f kb/s ack %4.0f ms enc %4.1f ms cpu %3.0f%% video %d%%\n",
			shape.name, tiles.fps, tiles.kbps, tiles.ackP50, tiles.encodeMs, tiles.cpuPct,
			video.fps, video.kbps, video.ackP50, video.encodeMs, video.cpuPct, video.videoPct)
	}
	t.Log(table.String())
}
