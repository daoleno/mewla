//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
		// Its own tab per mode, opened last and brought to the front: the page
		// scrolls itself once, 4 s after it starts drawing, for 4 s, and stays
		// still; no input of any kind. A fresh session per mode so one's
		// leftovers do not reach the other.
		openScrollFixture(t, ctx, backend, mode.name, 4000, 4000, false)
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
		if r.noSharpen {
			fmt.Fprintf(&table, "(tiles went out at the viewer's quality: no sharpening update followed; the picture built while moving is the sharp one)\n")
		}
		fmt.Fprintf(&table, "%-8s %9.0f %9.0f %9.0f %9.2f %9.1f %8.2f %9d   scroll %d -> %d\n", mode.name, r.gapMs, r.fullMs, r.coverage*100, r.meanDiff, r.psnr, r.edgeRatio, vp, r.pos0, r.pos1)
	}
	t.Log(table.String())
}

// openScrollFixture opens the fixture page in Safari with open -a Safari and
// brings Safari to the front through the accessibility API (AXFrontmost; no
// posted input event), the same call Screen's own raise uses.
func openScrollFixture(t *testing.T, ctx context.Context, backend Backend, label string, delay, run int, loop bool) {
	t.Helper()
	page := filepath.Join(t.TempDir(), "mewla-scroll-fixture-"+label+".html")
	if err := os.WriteFile(page, []byte(scrollFixtureHTML(label, delay, run, loop)), 0o600); err != nil {
		t.Fatal(err)
	}
	u := url.URL{Scheme: "file", Path: page}
	if err := exec.Command("open", "-a", "Safari", u.String()).Run(); err != nil {
		t.Skipf("no Safari: %v", err)
	}
	raiser, ok := backend.(interface {
		Apps(context.Context) ([]App, error)
		RaiseApp(context.Context, int) error
	})
	if !ok {
		t.Logf("this backend cannot bring Safari forward")
		return
	}
	for i := 0; i < 20; i++ { // Safari may need a moment to be running
		apps, err := raiser.Apps(ctx)
		if err == nil {
			for _, a := range apps {
				if a.Name == "Safari" {
					if err := raiser.RaiseApp(ctx, a.PID); err != nil {
						t.Logf("could not bring Safari forward: %v", err)
					}
					return
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("Safari is not in the app list")
}

// TestRealScreenViewerEndToEnd is the check that does not need a phone: the
// app's real viewer (noVNC with its H.264 path) runs in headless Chrome
// against this Mac's real capture, VideoToolbox video and the same RFB
// server, while the Safari fixture page scrolls itself; the viewer's decoded
// canvas is compared with a direct capture. Needs Chrome (MEWLA_CHROME, or
// Google Chrome in /Applications). MEWLA_SCREEN_REAL_BENCH=1.
func TestRealScreenViewerEndToEnd(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_REAL_BENCH") == "" {
		t.Skip("opens a Safari window: set MEWLA_SCREEN_REAL_BENCH=1")
	}
	chrome := os.Getenv("MEWLA_CHROME")
	if chrome == "" {
		for _, c := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
			if _, err := os.Stat(c); err == nil {
				chrome = c
				break
			}
		}
	}
	if chrome == "" {
		t.Skip("SKIP: no Chrome on this Mac (set MEWLA_CHROME); the end-to-end viewer check was not run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := newBackend()
	if capability := backend.Capability(ctx); !capability.Ready() {
		t.Skipf("Screen is not ready here: %+v", capability)
	}
	openScrollFixture(t, ctx, backend, "viewer", 7000, 4000, false)
	session, err := backend.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	r := runViewerE2E(t, ctx, session, newVTEncoder, chrome, os.Getenv("MEWLA_SCREEN_BENCH_OUT"))
	t.Log("\n" + reportE2E(t, "video", true, r))
}
