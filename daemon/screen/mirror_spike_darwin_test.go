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
//	MEWLA_IPHONE_SPIKE=<dir for pictures> [MEWLA_IPHONE_SPIKE_INPUT=1] go test ./screen -run IPhoneMirrorSpike -v
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
	s := NewService(t.TempDir())
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
		return lines, changed
	}
	lines, _ := shot("first")
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
		err := fn()
		time.Sleep(1200 * time.Millisecond)
		_, changed := shot(label)
		say("   %s: err %v, changed %v", label, err, changed)
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
	for _, l := range lines {
		if l.Box.Min.Y > h/8 && l.Box.Max.Y < h*7/8 {
			mid := image.Pt((l.Box.Min.X+l.Box.Max.X)/2, (l.Box.Min.Y+l.Box.Max.Y)/2)
			say("tapping %q at %v", l.Text, mid)
			step("tap", func() error { return s.MirrorTap(ctx, "spike", mid.X, mid.Y-l.Box.Dy()*2, 0) })
			step("home-after-tap", func() error { return s.MirrorKey(ctx, "spike", MirrorHome) })
			break
		}
	}
	shot("last")
}
