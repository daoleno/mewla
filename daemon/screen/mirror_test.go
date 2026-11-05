package screen

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"
)

// mirrorApps are the person's app in front and iPhone Mirroring behind it.
func mirrorApps() []FakeApp {
	apps := parkingApps()
	mirror := FakeApp{App: App{Name: "iPhone 镜像", PID: 404, BundleID: MirrorBundleID,
		Windows: []AppWindow{{ID: 44, Bounds: image.Rect(1200, 200, 1200+660, 200+1420)}}}}
	mirror.Window = mirror.Windows[0].Bounds
	return append(apps, mirror)
}

// mirrorIndex is where the mirror is among the fake's apps.
func mirrorIndex(backend *FakeBackend) int {
	for i, app := range backend.RunningApps {
		if app.BundleID == MirrorBundleID {
			return i
		}
	}
	return -1
}

func newMirrorService(t *testing.T) (*Service, *FakeBackend) {
	t.Helper()
	s, backend, _ := newAgentService(t)
	backend.RunningApps = mirrorApps()
	backend.AgentDisplay = true
	backend.SkyLight = true
	return s, backend
}

// A Worker's iPhone actions go to the mirror's window on the agent display,
// by points on its own picture, through the window server: never the
// pointer, and never the app in front.
func TestIPhoneActionsReachTheMirrorBehindThePersonsApp(t *testing.T) {
	s, backend := newMirrorService(t)
	ctx := context.Background()
	shot, err := s.MirrorShot(ctx, "%7")
	if err != nil {
		t.Fatal(err)
	}
	if shot.Rect.Dx() != 660 || shot.Rect.Dy() != 1420 {
		t.Fatalf("picture %v", shot.Rect)
	}
	if !backend.Parked[404] {
		t.Fatal("the mirror isn't on the agent display")
	}
	if backend.Parked[101] {
		t.Fatal("the person's app was moved")
	}
	if err := s.MirrorTap(ctx, "%7", 100, 200, 0); err != nil {
		t.Fatal(err)
	}
	click := backend.AppClicks[len(backend.AppClicks)-1]
	window := backend.RunningApps[mirrorIndex(backend)].Windows[0].Bounds
	if click.PID != 404 || click.Window != 44 || click.X != window.Min.X+100 || click.Y != window.Min.Y+200 {
		t.Fatalf("click %+v on window %v", click, window)
	}
	if err := s.MirrorTap(ctx, "%7", 100, 200, 800*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.MirrorSwipe(ctx, "%7", 300, 1000, 300, 400, SwipeScroll); err != nil {
		t.Fatal(err)
	}
	if err := s.MirrorType(ctx, "%7", "Hi"); err != nil {
		t.Fatal(err)
	}
	if err := s.MirrorType(ctx, "%7", "你好"); err != nil {
		t.Fatal(err)
	}
	if err := s.MirrorKey(ctx, "%7", MirrorReturn); err != nil {
		t.Fatal(err)
	}
	in := backend.MirrorInputs
	if len(in) != 5 {
		t.Fatalf("inputs %+v", in)
	}
	if in[0].Kind != "hold" || in[0].Hold != 800*time.Millisecond {
		t.Fatalf("hold %+v", in[0])
	}
	if in[1].Kind != "swipe" || in[1].DY != -600 || in[1].How != SwipeScroll || in[1].X != window.Min.X+300 {
		t.Fatalf("swipe %+v", in[1])
	}
	if in[2].Kind != "strokes" || len(in[2].Strokes) != 2 || in[2].Strokes[0].Flags != macFlagShift || in[2].Strokes[1].Text != 'i' {
		t.Fatalf("typed %+v", in[2])
	}
	if in[3].Kind != "paste" || in[3].Text != "你好" {
		t.Fatalf("pasted %+v", in[3])
	}
	if in[4].Kind != "strokes" || in[4].Strokes[0].Code != 36 {
		t.Fatalf("return %+v", in[4])
	}
	pointers, _ := backend.Snapshot()
	if len(pointers) != 0 {
		t.Fatalf("the pointer moved: %v", pointers)
	}
	if err := s.MirrorTap(ctx, "%7", 5000, 5000, 0); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("a point off the picture: %v", err)
	}
}

// One Worker has the iPhone; another is told which.
func TestTheIPhoneHasOneWorkerAtATime(t *testing.T) {
	s, _ := newMirrorService(t)
	s.SetWorkerNames(func(id string) string { return map[string]string{"%7": "Shopper"}[id] })
	ctx := context.Background()
	if err := s.MirrorTap(ctx, "%7", 10, 10, 0); err != nil {
		t.Fatal(err)
	}
	err := s.MirrorTap(ctx, "%8", 10, 10, 0)
	if err == nil || !strings.Contains(err.Error(), "Shopper") || !strings.Contains(err.Error(), "using the iPhone") {
		t.Fatalf("second Worker: %v", err)
	}
	if info := s.MirrorInfo(ctx); info.Worker != "%7" || !info.Running {
		t.Fatalf("info %+v", info)
	}
	s.MirrorRelease("%7")
	if err := s.MirrorTap(ctx, "%8", 10, 10, 0); err != nil {
		t.Fatalf("after the first let go: %v", err)
	}
}

// Home, the app switcher and Spotlight are the mirror's menu items, chosen
// without keys; a missing item falls back to its shortcut.
func TestIPhoneMenuKeys(t *testing.T) {
	s, backend := newMirrorService(t)
	backend.RunningApps[mirrorIndex(backend)].Menu = []FakeMenuItem{{Path: "View > Home Screen", Char: "1"}}
	ctx := context.Background()
	if err := s.MirrorKey(ctx, "%7", MirrorHome); err != nil {
		t.Fatal(err)
	}
	if len(backend.MenuPresses) != 1 || len(backend.MirrorInputs) != 0 {
		t.Fatalf("home: menu %v inputs %+v", backend.MenuPresses, backend.MirrorInputs)
	}
	if err := s.MirrorKey(ctx, "%7", MirrorSpotlight); err != nil {
		t.Fatal(err)
	}
	if len(backend.MirrorInputs) != 1 || backend.MirrorInputs[0].Strokes[0].Flags != macFlagCommand || backend.MirrorInputs[0].Strokes[0].Code != macANSIKeys['3'].code {
		t.Fatalf("spotlight: %+v", backend.MirrorInputs)
	}
}

// A mirror the person has in front is theirs.
func TestTheMirrorInFrontIsThePersons(t *testing.T) {
	s, backend := newMirrorService(t)
	backend.RunningApps[0].Active = false
	backend.RunningApps[mirrorIndex(backend)].Active = true
	_, err := s.MirrorShot(context.Background(), "%7")
	if !errors.Is(err, ErrMirrorInFront) {
		t.Fatalf("got %v", err)
	}
}

// A mirror that isn't running is opened, and the front goes back to the
// person's app.
func TestTheMirrorIsOpenedWhenItIsNotRunning(t *testing.T) {
	s, backend := newMirrorService(t)
	i := mirrorIndex(backend)
	mirror := backend.RunningApps[i]
	backend.RunningApps = backend.RunningApps[:i]
	mirror.Active = true
	backend.InstalledApps = []FakeInstalledApp{{FakeApp: mirror, Bundle: MirrorBundleID}}
	person := backend.RunningApps[0].PID
	if _, err := s.MirrorShot(context.Background(), "%7"); err != nil {
		t.Fatal(err)
	}
	if len(backend.Launched) != 1 || backend.Launched[0] != MirrorBundleID {
		t.Fatalf("launched %v", backend.Launched)
	}
	if len(backend.Raised) != 1 || backend.Raised[0] != person {
		t.Fatalf("the front went to %v, not back to %d", backend.Raised, person)
	}
}

// mewla_computer leaves the mirror to the iPhone tools.
func TestTheComputerToolsLeaveTheMirrorAlone(t *testing.T) {
	s, _ := newMirrorService(t)
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", App: "iPhone Mirroring", Coordinate: []int{5, 5}})
	if !errors.Is(err, ErrMirrorForIPhoneTools) {
		t.Fatalf("got %v", err)
	}
}

func TestAsciiStrokesAndReadingOrder(t *testing.T) {
	if _, ok := asciiStrokes("héllo"); ok {
		t.Fatal("é has no key")
	}
	strokes, ok := asciiStrokes("a\nB")
	if !ok || len(strokes) != 3 || strokes[1].Code != 36 || strokes[2].Flags != macFlagShift {
		t.Fatalf("%+v", strokes)
	}
	lines := []TextLine{
		{Text: "right", Box: image.Rect(200, 102, 260, 122)},
		{Text: "below", Box: image.Rect(0, 200, 60, 220)},
		{Text: "left", Box: image.Rect(10, 100, 60, 120)},
	}
	sortLines(lines)
	if lines[0].Text != "left" || lines[1].Text != "right" || lines[2].Text != "below" {
		t.Fatalf("%+v", lines)
	}
}
