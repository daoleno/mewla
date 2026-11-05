//go:build darwin && !screenfake

package screen

import (
	"context"
	"fmt"
	"hash/fnv"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dumpAX prints the mirror's accessibility tree, set by the spike.
var dumpAX = func(string) {}

func loadPNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)
	return img
}

// Vision reads Chinese and English off a picture, each line where it is
// drawn: boxes from the top left, in the picture's pixels.
func TestVisionReadsChineseAndEnglishWhereTheyAre(t *testing.T) {
	if err := loadMac(); err != nil {
		t.Skip(err)
	}
	img := loadPNG(t, filepath.Join("testdata", "mirror-ocr.png"))
	lines, err := (&macBackend{}).ReadText(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]image.Point{"设置": {80, 85}, "Safari": {460, 85}, "微信": {80, 505}}
	for text, at := range want {
		found := false
		for _, l := range lines {
			if strings.Contains(l.Text, text) {
				found = true
				mid := image.Pt((l.Box.Min.X+l.Box.Max.X)/2, (l.Box.Min.Y+l.Box.Max.Y)/2)
				if d := mid.Sub(at); d.X*d.X+d.Y*d.Y > 60*60 {
					t.Errorf("%q is at %v, drawn near %v", text, l.Box, at)
				}
			}
		}
		if !found {
			t.Errorf("%q not read; read %+v", text, lines)
		}
	}
	if len(lines) > 0 && !strings.Contains(lines[0].Text, "设置") {
		t.Errorf("not in reading order: %+v", lines)
	}
}

// A spike, not a test: what iPhone Mirroring on this Mac takes. Run it on a
// Mac with an iPhone set up for iPhone Mirroring, locked and nearby:
//
//	MEWLA_IPHONE_SPIKE=<dir for pictures> [MEWLA_IPHONE_SPIKE_INPUT=1] [MEWLA_IPHONE_SPIKE_NOPARK=1] go test ./screen -run IPhoneMirrorSpike -v
//
// NOPARK leaves the mirror on the person's display, behind their windows,
// instead of the agent display. Around every input step it prints the app
// in front and the pointer, so input that took the person's focus or moved
// their pointer shows. The mirror's own accessibility tree is printed at the
// start and whenever a reading has few lines.
//
// Without INPUT it only opens the mirror, moves it to the agent display and
// reads it. With INPUT it also presses Home, opens Spotlight, types "clock"
// with key codes and "时钟" by paste, swipes the home screen each way a
// swipe can be sent and taps the first label it read, saying after each
// whether the iPhone's picture changed. Every picture is saved in the dir.
func TestIPhoneMirrorSpike(t *testing.T) {
	dir := os.Getenv("MEWLA_IPHONE_SPIKE")
	if dir == "" {
		t.Skip("set MEWLA_IPHONE_SPIKE=<dir> to run")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	say := func(format string, args ...any) {
		fmt.Printf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
	ctx := context.Background()
	mirrorNoPark = os.Getenv("MEWLA_IPHONE_SPIKE_NOPARK") != ""
	defer func() { mirrorNoPark = false }()
	s := NewService(t.TempDir())
	if capability, err := s.AgentReady(ctx); err != nil {
		say("not ready: %v (%+v). This process needs Screen Recording and Accessibility; run it from Mewla's own terminal.", err, capability)
		t.Fatal(err)
	}
	say("no park: %v", mirrorNoPark)
	t.Cleanup(func() { s.MirrorRelease("spike"); s.StopAgent(); time.Sleep(500 * time.Millisecond) })
	say("macOS %s; before: %+v", productVersion(), s.MirrorInfo(ctx))
	n := 0
	var last uint64
	shot := func(label string) ([]TextLine, bool) {
		n++
		started := time.Now()
		img, err := s.MirrorShot(ctx, "spike")
		if err != nil {
			say("%02d %s: shot failed: %v", n, label, err)
			return nil, false
		}
		took := time.Since(started)
		path := filepath.Join(dir, fmt.Sprintf("%02d-%s.png", n, label))
		if f, err := os.Create(path); err == nil {
			_ = png.Encode(f, img)
			f.Close()
		}
		started = time.Now()
		lines, err := s.MirrorRead(ctx, img)
		var texts []string
		for _, l := range lines {
			texts = append(texts, fmt.Sprintf("%q@%d,%d", l.Text, l.Box.Min.X, l.Box.Min.Y))
		}
		h := fnv.New64a()
		h.Write(img.Pix)
		sum := h.Sum64()
		changed := sum != last
		last = sum
		say("%02d %s: %dx%d shot %s, read %s (%v), changed %v: %s", n, label, img.Rect.Dx(), img.Rect.Dy(), took.Round(time.Millisecond), time.Since(started).Round(time.Millisecond), err, changed, strings.Join(texts, " "))
		if len(lines) <= 8 {
			dumpAX(label)
		}
		return lines, changed
	}
	dumpAX = func(label string) {
		s.agentMu.Lock()
		agent := s.agents["spike"]
		s.agentMu.Unlock()
		if agent == nil || agent.apps == nil {
			return
		}
		app, err := findMirror(ctx, agent.apps)
		if err != nil {
			return
		}
		tree, err := agent.apps.ReadApp(ctx, app.PID, 200)
		if err != nil {
			say("   ax %s: %v", label, err)
			return
		}
		defer agent.apps.Release(tree)
		for _, e := range tree.Elements {
			say("   ax %s: %s%s %q %q %v %v", label, strings.Repeat("  ", e.Depth), e.Role, e.Title, e.Value, e.Frame, e.Actions)
		}
	}
	lines, _ := shot("first")
	dumpAX("first")
	s.agentMu.Lock()
	agent := s.agents["spike"]
	s.agentMu.Unlock()
	if agent != nil && agent.apps != nil {
		if app, err := findMirror(ctx, agent.apps); err == nil {
			say("mirror pid %d active %v parked %v windows %+v", app.PID, app.Active, agent.isParked(app.PID), app.Windows)
		}
	}
	time.Sleep(2 * time.Second)
	shot("still")
	if os.Getenv("MEWLA_IPHONE_SPIKE_INPUT") == "" {
		return
	}
	step := func(label string, fn func() error) bool {
		front, pointer := workspaceFrontPID(), pointerAt()
		err := fn()
		frontAfter, pointerAfter := workspaceFrontPID(), pointerAt()
		time.Sleep(1200 * time.Millisecond)
		frontLater := workspaceFrontPID()
		_, changed := shot(label)
		say("   %s: err %v, changed %v; front %d -> %d -> %d (took focus: %v); pointer moved: %v", label, err, changed, front, frontAfter, frontLater, frontAfter != front || frontLater != front, pointer != pointerAfter)
		return changed
	}
	step("home", func() error { return s.MirrorKey(ctx, "spike", MirrorHome) })
	step("spotlight", func() error { return s.MirrorKey(ctx, "spike", MirrorSpotlight) })
	step("type-clock", func() error { return s.MirrorType(ctx, "spike", "clock") })
	step("select-all-delete", func() error {
		if err := s.MirrorKey(ctx, "spike", MirrorSelectAll); err != nil {
			return err
		}
		return s.MirrorKey(ctx, "spike", MirrorDelete)
	})
	step("paste-shizhong", func() error { return s.MirrorType(ctx, "spike", "时钟") })
	step("escape", func() error { return s.MirrorKey(ctx, "spike", MirrorEscape) })
	step("home-again", func() error { return s.MirrorKey(ctx, "spike", MirrorHome) })
	img, err := s.MirrorShot(ctx, "spike")
	if err != nil {
		t.Fatal(err)
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for _, how := range []SwipeHow{SwipeScroll, SwipeDrag, SwipeWheel} {
		step("swipe-left-"+string(how), func() error { return s.MirrorSwipe(ctx, "spike", w*4/5, h/2, w/5, h/2, how) })
		step("swipe-right-"+string(how), func() error { return s.MirrorSwipe(ctx, "spike", w/5, h/2, w*4/5, h/2, how) })
	}
	step("app-switcher", func() error { return s.MirrorKey(ctx, "spike", MirrorAppSwitcher) })
	step("home-before-tap", func() error { return s.MirrorKey(ctx, "spike", MirrorHome) })
	lines, _ = shot("before-tap")
	tapped := false
	for _, name := range []string{"设置", "Settings", "时钟", "Clock"} {
		for _, l := range lines {
			if strings.TrimSpace(l.Text) != name {
				continue
			}
			// The label sits under its icon: tap the icon.
			mid := image.Pt((l.Box.Min.X+l.Box.Max.X)/2, l.Box.Min.Y-l.Box.Dy()*2)
			say("tapping the icon above %q at %v", l.Text, mid)
			step("tap-"+name, func() error { return s.MirrorTap(ctx, "spike", mid.X, mid.Y, 0) })
			step("home-after-tap", func() error { return s.MirrorKey(ctx, "spike", MirrorHome) })
			tapped = true
			break
		}
		if tapped {
			break
		}
	}
	if !tapped {
		say("no Settings or Clock label on this home page: no tap")
	}
	shot("last")
}
