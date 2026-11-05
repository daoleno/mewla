package iphone

import (
	"context"
	"errors"
	"image"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/phone"
)

// fakeMirror is iPhone Mirroring showing a 600 x 1300 picture whose text is
// lines; it records what it was sent.
type fakeMirror struct {
	mu       sync.Mutex
	lines    []Line
	sent     []string
	released []string
	stops    []string
	noMac    bool
	// onKey changes the screen as the iPhone would.
	onKey func(name string)
}

func (m *fakeMirror) Info(context.Context) Info {
	return Info{Supported: !m.noMac, Running: true, Window: true}
}

func (m *fakeMirror) Shot(context.Context, string) (*image.RGBA, error) {
	return image.NewRGBA(image.Rect(0, 0, 600, 1300)), nil
}

func (m *fakeMirror) Read(context.Context, *image.RGBA) ([]Line, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Line(nil), m.lines...), nil
}

func (m *fakeMirror) note(s string) {
	m.mu.Lock()
	m.sent = append(m.sent, s)
	m.mu.Unlock()
}

func (m *fakeMirror) Tap(_ context.Context, _ string, x, y int, hold time.Duration) error {
	m.note("tap " + itoa(x) + "," + itoa(y) + map[bool]string{true: " long", false: ""}[hold > 0])
	return nil
}

func (m *fakeMirror) Swipe(_ context.Context, _ string, x1, y1, x2, y2 int) error {
	m.note("swipe " + itoa(x1) + "," + itoa(y1) + "->" + itoa(x2) + "," + itoa(y2))
	return nil
}

func (m *fakeMirror) Type(_ context.Context, _ string, text string) error {
	m.note("type " + text)
	return nil
}

func (m *fakeMirror) Key(_ context.Context, _ string, name string) error {
	m.note("key " + name)
	if m.onKey != nil {
		m.onKey(name)
	}
	return nil
}

func (m *fakeMirror) Stop(worker string) {
	m.mu.Lock()
	m.stops = append(m.stops, worker)
	m.mu.Unlock()
}

func (m *fakeMirror) Release(worker string) {
	m.mu.Lock()
	m.released = append(m.released, worker)
	m.mu.Unlock()
}

func itoa(v int) string { return strconv.Itoa(v) }

func instant(context.Context, time.Duration) error { return nil }

func newAgent(m *fakeMirror) (*phone.Agent, *Phone) {
	p := New(m, Options{Sleep: instant, Settle: time.Millisecond})
	return phone.NewDeviceAgent(p, nil, Profile), p
}

func home() []Line {
	return []Line{
		{Text: "设置", Box: image.Rect(60, 300, 180, 340)},
		{Text: "Safari", Box: image.Rect(300, 300, 420, 340)},
		{Text: "9:41", Box: image.Rect(40, 20, 120, 50)},
	}
}

// The screen is the text on the mirror's picture, at most 1000 pixels on its
// long edge, and a tap by id lands on the picture where the text is.
func TestTheScreenIsItsTextAndTapsLandOnIt(t *testing.T) {
	m := &fakeMirror{lines: home()}
	a, _ := newAgent(m)
	ctx := context.Background()
	res, err := a.Act(ctx, "%7", "Shopper", phone.AgentAction{Action: phone.ActScreen})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Screen 462x1000") || !strings.Contains(res.Text, `"设置"`) || !strings.Contains(res.Text, `"Safari"`) {
		t.Fatalf("screen:\n%s", res.Text)
	}
	if _, err := a.Act(ctx, "%7", "Shopper", phone.AgentAction{Action: phone.ActTap, Match: "safari"}); err != nil {
		t.Fatal(err)
	}
	if got := m.sent[0]; got != "tap 360,320" {
		t.Fatalf("tap went to %q", got)
	}
	if _, err := a.Act(ctx, "%7", "Shopper", phone.AgentAction{Action: phone.ActTap, X: 100, Y: 200, Long: true}); err != nil {
		t.Fatal(err)
	}
	if got := m.sent[1]; got != "tap 130,260 long" {
		t.Fatalf("long press went to %q", got)
	}
}

// Typing with replace clears the field first; keys are the iPhone's, and
// back is refused with what to do instead.
func TestTypingAndKeys(t *testing.T) {
	m := &fakeMirror{lines: home()}
	a, _ := newAgent(m)
	ctx := context.Background()
	if _, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActType, Text: "你好", Replace: true, Submit: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"key select_all", "key delete", "type 你好", "key return"}
	if strings.Join(m.sent, "|") != strings.Join(want, "|") {
		t.Fatalf("sent %v", m.sent)
	}
	_, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActKey, Key: "back"})
	if err == nil || !strings.Contains(err.Error(), "no back key") {
		t.Fatalf("back: %v", err)
	}
	if _, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActKey, Key: "home"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActApps}); err == nil || !strings.Contains(err.Error(), "cannot list") {
		t.Fatalf("apps: %v", err)
	}
}

// An app opens through Spotlight by its name.
func TestLaunchGoesThroughSpotlight(t *testing.T) {
	m := &fakeMirror{lines: home()}
	a, _ := newAgent(m)
	res, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActLaunch, App: "微信"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"key home", "key spotlight", "type 微信", "key return"}
	if strings.Join(m.sent, "|") != strings.Join(want, "|") {
		t.Fatalf("sent %v", m.sent)
	}
	if !strings.Contains(res.Text, "through Spotlight") {
		t.Fatalf("answer:\n%s", res.Text)
	}
}

// A swipe by direction moves through the middle of the screen, in picture pixels.
func TestSwipeByDirection(t *testing.T) {
	m := &fakeMirror{lines: home()}
	a, _ := newAgent(m)
	if _, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActSwipe, Direction: "up"}); err != nil {
		t.Fatal(err)
	}
	if got := m.sent[0]; got != "swipe 300,975->300,325" {
		t.Fatalf("swipe %q", got)
	}
}

// The keyboard's letters are not elements; the reading says it is open.
func TestTheKeyboardIsLeftOut(t *testing.T) {
	lines := []Line{{Text: "搜索", Box: image.Rect(40, 100, 200, 140)}}
	for i, r := range "qwertyuiop" {
		lines = append(lines, Line{Text: string(r), Box: image.Rect(10+i*55, 900, 50+i*55, 950)})
	}
	lines = append(lines, Line{Text: "asdfghjkl", Box: image.Rect(30, 1000, 560, 1040)})
	m := &fakeMirror{lines: lines}
	a, _ := newAgent(m)
	res, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActScreen})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "keyboard open") || strings.Contains(res.Text, `"q"`) || !strings.Contains(res.Text, `"搜索"`) {
		t.Fatalf("screen:\n%s", res.Text)
	}
	// A few short words at the bottom are the app's own.
	m.lines = []Line{{Text: "OK", Box: image.Rect(250, 1200, 350, 1240)}}
	res, _ = a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActScreen})
	if !strings.Contains(res.Text, `"OK"`) {
		t.Fatalf("screen:\n%s", res.Text)
	}
}

// The mirror's own screens are refusals or notes, not the iPhone's screen.
func TestTheMirrorsOwnScreens(t *testing.T) {
	m := &fakeMirror{lines: []Line{{Text: "iPhone 使用中", Box: image.Rect(100, 600, 500, 650)}}}
	a, _ := newAgent(m)
	ctx := context.Background()
	_, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActScreen})
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("in use: %v", err)
	}
	// What an app may show too is a note, never a refusal.
	m.lines = []Line{{Text: "使用触控 ID 来打开 iPhone 镜像", Box: image.Rect(50, 600, 550, 650)}}
	if res, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActScreen}); err != nil || !strings.Contains(res.Text, "never type it") {
		t.Fatalf("auth: %v\n%s", err, res.Text)
	}
	m.lines = []Line{{Text: "欢迎", Box: image.Rect(50, 600, 550, 650)}, {Text: "继续", Box: image.Rect(250, 700, 350, 740)}}
	if res, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActScreen}); err != nil || strings.Contains(res.Text, "Mirroring") {
		t.Fatalf("an app's Continue: %v\n%s", err, res.Text)
	}
	m.lines = []Line{{Text: "无法连接到 iPhone", Box: image.Rect(50, 600, 550, 650)}, {Text: "重试", Box: image.Rect(250, 700, 350, 740)}}
	res, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActScreen})
	if err != nil || !strings.Contains(res.Text, "Try Again") || !strings.Contains(res.Text, `"重试"`) {
		t.Fatalf("disconnected: %v\n%s", err, res.Text)
	}
	m.noMac = true
	if _, err := a.Act(ctx, "%7", "", phone.AgentAction{Action: phone.ActScreen}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("no mirror: %v", err)
	}
}

// One Worker at a time; a quiet one loses it.
func TestOneWorkerAtATime(t *testing.T) {
	m := &fakeMirror{lines: home()}
	now := time.Unix(1000, 0)
	p := New(m, Options{Sleep: instant, Settle: time.Millisecond, Now: func() time.Time { return now }})
	a := phone.NewDeviceAgent(p, nil, Profile)
	ctx := context.Background()
	if _, err := a.Act(ctx, "%7", "Shopper", phone.AgentAction{Action: phone.ActScreen}); err != nil {
		t.Fatal(err)
	}
	_, err := a.Act(ctx, "%8", "", phone.AgentAction{Action: phone.ActScreen})
	if !errors.Is(err, &phone.Error{Code: phone.CodeBusy}) || !strings.Contains(err.Error(), "Shopper") {
		t.Fatalf("second: %v", err)
	}
	if st := p.Status(); st.Active == nil || st.Active.Worker != "%7" {
		t.Fatalf("status %+v", st)
	}
	now = now.Add(DefaultIdle + time.Second)
	if _, err := a.Act(ctx, "%8", "", phone.AgentAction{Action: phone.ActScreen}); err != nil {
		t.Fatalf("after the first went quiet: %v", err)
	}
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return len(m.released) == 1 && m.released[0] == "%7" })
}

// A handoff waits for the person's Done, or for the iPhone to be unlocked and locked again.
func TestHandoff(t *testing.T) {
	m := &fakeMirror{lines: home()}
	var told []string
	p := New(m, Options{Sleep: instant, Settle: time.Millisecond, Notify: func(_, _, reason string) { told = append(told, reason) }})
	a := phone.NewDeviceAgent(p, nil, Profile)
	done := make(chan error, 1)
	go func() {
		_, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActHandoff, Reason: "Sign in to Taobao"})
		done <- err
	}()
	waitFor(t, func() bool { return p.HandoffDone() })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || !strings.Contains(told[0], "Sign in to Taobao") {
		t.Fatalf("told %v", told)
	}

	// Unlocked, then locked again.
	calls := 0
	m2 := &unlocking{fakeMirror: fakeMirror{lines: home()}, calls: &calls}
	p2 := New(m2, Options{Sleep: instant, Settle: time.Millisecond})
	res, err := phone.NewDeviceAgent(p2, nil, Profile).Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActHandoff, Reason: "Pay"})
	if err != nil || !strings.Contains(res.Text, "locked it again") {
		t.Fatalf("%v\n%s", err, res.Text)
	}
}

// unlocking shows the iPhone in use for its second and third readings.
type unlocking struct {
	fakeMirror
	calls *int
}

func (u *unlocking) Read(ctx context.Context, img *image.RGBA) ([]Line, error) {
	*u.calls++
	if *u.calls == 2 || *u.calls == 3 {
		return []Line{{Text: "iPhone in Use", Box: image.Rect(100, 600, 500, 650)}}, nil
	}
	return u.fakeMirror.Read(ctx, img)
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The person's Stop ends a handoff the agent waits on, and the agent is
// refused from then on, until iPhone use is turned off and on.
func TestStopEndsTheAgentAndKeepsItOff(t *testing.T) {
	m := &fakeMirror{lines: home()}
	p := New(m, Options{Settle: time.Millisecond, Sleep: func(ctx context.Context, d time.Duration) error {
		select {
		case <-time.After(time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	a := phone.NewDeviceAgent(p, nil, Profile)
	done := make(chan error, 1)
	go func() {
		_, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActHandoff, Reason: "Pay"})
		done <- err
	}()
	waitFor(t, func() bool { return p.Active() != nil })
	p.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("handoff ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the handoff went on after Stop")
	}
	if _, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActScreen}); !errors.Is(err, ErrStopped) {
		t.Fatalf("after Stop: %v", err)
	}
	m.mu.Lock()
	stops := append([]string(nil), m.stops...)
	m.mu.Unlock()
	if len(stops) == 0 || stops[0] != "%7" {
		t.Fatalf("the Mac session was not stopped: %v", stops)
	}
	// Another Worker may go on; the switch off and on lets this one again.
	if _, err := a.Act(context.Background(), "%8", "", phone.AgentAction{Action: phone.ActScreen}); err != nil {
		t.Fatalf("another Worker: %v", err)
	}
	p.Release()
	if _, err := a.Act(context.Background(), "%7", "", phone.AgentAction{Action: phone.ActScreen}); err != nil {
		t.Fatalf("after the switch went off and on: %v", err)
	}
	if pic, at := p.Picture(); len(pic) == 0 || at.IsZero() {
		t.Fatal("no picture to watch")
	}
}
