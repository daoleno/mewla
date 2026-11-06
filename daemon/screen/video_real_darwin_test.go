//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRealScreenVideoOverSimulatedLinks measures the real thing: this Mac's
// own capture, a long page that scrolls itself in Safari (requestAnimationFrame
// in the page; no wheel, key or click events), the real hardware encoder, over
// the same simulated links as the fake-screen bench. It opens one Safari tab
// (titled "mewla-scroll-fixture loop", left open) and needs about a minute;
// run it when nobody is using this Mac. MEWLA_SCREEN_REAL_BENCH=1 and -v.
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

	// The page scrolls itself up and down (requestAnimationFrame); no input
	// reaches the Mac. Its tab is titled mewla-scroll-fixture loop.
	openScrollFixture(t, ctx, backend, "loop", 0, 0, true)
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
	_ = first
	drive := func(step int) {}

	shapes := []linkShape{
		{"LAN 100 Mb/s, 2 ms", 2 * time.Millisecond, 100e6 / 8, false},
		{"relay 20 Mb/s, 150 ms RTT", 75 * time.Millisecond, 20e6 / 8, false},
		{"mobile 4 Mb/s, 80 ms RTT", 40 * time.Millisecond, 4e6 / 8, false},
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
