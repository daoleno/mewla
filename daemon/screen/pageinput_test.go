package screen

import (
	"errors"
	"image"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

// The person's keys become the DevTools key events a page takes: a letter
// types itself (shifted, its capital), Enter and Backspace are keys with their
// codes, held modifiers ride on every event, and Command with a letter on a
// Mac is the editing command, not text.
func TestPageKeysAreTheKeyEventsAPageTakes(t *testing.T) {
	mods := 0
	key := func(keysym uint32, down bool) map[string]any {
		t.Helper()
		in, ok := pageKey(keysym, down, &mods)
		if !ok || in.Method != "Input.dispatchKeyEvent" {
			t.Fatalf("keysym %#x: %+v %v", keysym, in, ok)
		}
		return in.Params
	}
	want := func(got, want map[string]any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	want(key('h', true), map[string]any{"type": "keyDown", "key": "h", "code": "KeyH", "windowsVirtualKeyCode": 72, "text": "h", "modifiers": 0})
	want(key('h', false), map[string]any{"type": "keyUp", "key": "h", "code": "KeyH", "windowsVirtualKeyCode": 72, "modifiers": 0})
	want(key(0xffe1, true), map[string]any{"type": "rawKeyDown", "key": "Shift", "code": "ShiftLeft", "windowsVirtualKeyCode": 16, "modifiers": pageShift})
	want(key('H', true), map[string]any{"type": "keyDown", "key": "H", "code": "KeyH", "windowsVirtualKeyCode": 72, "text": "H", "modifiers": pageShift})
	key(0xffe1, false)
	want(key(' ', true), map[string]any{"type": "keyDown", "key": " ", "code": "Space", "windowsVirtualKeyCode": 32, "text": " ", "modifiers": 0})
	want(key('7', true), map[string]any{"type": "keyDown", "key": "7", "code": "Digit7", "windowsVirtualKeyCode": 55, "text": "7", "modifiers": 0})
	want(key('@', true), map[string]any{"type": "keyDown", "key": "@", "text": "@", "modifiers": 0})
	want(key(0xff0d, true), map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "text": "\r", "modifiers": 0})
	want(key(0xff08, true), map[string]any{"type": "rawKeyDown", "key": "Backspace", "code": "Backspace", "windowsVirtualKeyCode": 8, "modifiers": 0})
	// Command (Super from the app's key bar, Meta from a desktop browser) with a letter.
	key(0xffeb, true)
	want(key('a', true), map[string]any{"type": "rawKeyDown", "key": "a", "code": "KeyA", "windowsVirtualKeyCode": 65, "modifiers": pageMeta, "commands": []string{"selectAll"}})
	key(0xffeb, false)
	if mods != 0 {
		t.Fatalf("modifiers stayed held: %d", mods)
	}
	if _, ok := pageKey(0xfe03, true, &mods); ok {
		t.Fatal("an unknown keysym became a key")
	}
}

// With the extension's way in, the view's click is a DevTools click at the
// window's point (in points from its top left, the capture's pixels halved on
// a Retina-like capture), keys are key events, and nothing goes through the
// window server; when the page way fails (an older extension), the click goes
// the window server's way instead.
func TestTheViewSendsInputThroughThePage(t *testing.T) {
	a, viewer, _ := newTestWindows(t)
	a.Opened("c1-1", "mh-signin", testCell)
	var sent []PageInput
	var refuse error
	a.SetPageInput(func(key string, in PageInput) error {
		if key != "c1-1" {
			t.Errorf("input for %q", key)
		}
		if refuse != nil {
			return refuse
		}
		sent = append(sent, in)
		return nil
	})
	view, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	session, bounds, release, _ := viewer.ViewAgentDisplay(t.Context(), "test")
	defer release()
	v := &windowView{inner: session, bounds: bounds, rect: func() (image.Rectangle, bool) { return a.rectOf("mh-signin") }, gone: a.owners["mh-signin"].gone, viewer: viewer}
	v.page = func(in PageInput) error { return a.page(a.keyOf("mh-signin"), in) }
	if _, err := v.Frame(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
	_ = v.Pointer(400, 300, 1)
	_ = v.Pointer(400, 300, 0)
	_ = v.Key('q', true)
	_ = v.Key('q', false)
	_ = v.Pointer(400, 300, 16)
	_ = v.Pointer(400, 300, 0)
	if len(sent) != 5 {
		t.Fatalf("sent %+v", sent)
	}
	press := sent[0].Params
	if sent[0].Method != "Input.dispatchMouseEvent" || press["type"] != "mousePressed" || press["x"] != 200.0 || press["y"] != 150.0 || press["button"] != "left" || press["clickCount"] != 1 {
		t.Fatalf("press %+v", sent[0])
	}
	if sent[1].Params["type"] != "mouseReleased" || sent[2].Params["text"] != "q" || sent[4].Params["type"] != "mouseWheel" || sent[4].Params["deltaY"] != 100 {
		t.Fatalf("sent %+v", sent)
	}
	if len(viewer.session.clicks) != 0 || len(viewer.session.keys) != 0 {
		t.Fatalf("input also went through the window server: %v %v", viewer.session.clicks, viewer.session.keys)
	}
	refuse = errors.New("too old")
	_ = v.Pointer(10, 20, 1)
	_ = v.Pointer(10, 20, 0)
	if len(viewer.session.clicks) != 1 {
		t.Fatalf("a click the page way refused did not go the other way: %v", viewer.session.clicks)
	}
}

// 2026-11-21: input from Screen that cannot reach the page of a window an
// agent left for the person was dropped in silence (a dead picture). Its views
// hear why, at once for one that opens later, and hear it end.
func TestInputThatCannotReachThePageIsSaid(t *testing.T) {
	a, _, _ := newTestWindows(t)
	a.Opened("c1-1", "mh-signin", testCell)
	refuse := errors.New("the Mewla extension in Chrome is too old to take input from Screen")
	a.SetPageInput(func(key string, in PageInput) error { return refuse })
	view, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	var mu sync.Mutex
	var heard []string
	view.Trouble(func(problem string) {
		mu.Lock()
		heard = append(heard, problem)
		mu.Unlock()
	})
	if _, err := view.Next(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
	input := func(msgs ...[]byte) {
		src, in := io.Pipe()
		done := make(chan error, 1)
		go func() { done <- view.Input(src) }()
		for _, m := range msgs {
			if _, err := in.Write(m); err != nil {
				t.Fatal(err)
			}
		}
		_ = in.Close()
		<-done
	}
	input([]byte{4, 1, 0, 0, 0, 0, 0, 'h'}, []byte{4, 0, 0, 0, 0, 0, 0, 'h'})
	mu.Lock()
	got := append([]string(nil), heard...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{refuse.Error()}) {
		t.Fatalf("the view heard %q", got)
	}
	// A view opened now hears it at once.
	later, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	var now string
	later.Trouble(func(problem string) { now = problem })
	if now != refuse.Error() {
		t.Fatalf("a later view heard %q", now)
	}
	// The browser takes input again: the problem ends; one the browser itself
	// reports (its page is not the agent's) is said, and its end too.
	refuse = nil
	a.SetPageInput(func(key string, in PageInput) error { return nil })
	input([]byte{4, 1, 0, 0, 0, 0, 0, 'h'})
	a.InputProblem("c1-1", "the page in front is not one the agent's browser tools hold")
	a.InputProblem("c1-1", "")
	a.InputProblem("c9-9", "a window nobody has")
	mu.Lock()
	got = append([]string(nil), heard...)
	mu.Unlock()
	want := []string{"the Mewla extension in Chrome is too old to take input from Screen", "", "the page in front is not one the agent's browser tools hold", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the view heard %q, want %q", got, want)
	}
}

// lockingViewer is a Mac-like backend: an agent display to view, a lock to
// read and a display to keep on.
type lockingViewer struct {
	*FakeBackend
	*fakeWindowViewer
}

func (lockingViewer) AgentDisplayAvailable() bool { return true }

// 2026-11-21: a person looking at a page an agent left long after its turn
// met a Mac whose display had slept or locked, and the view said nothing.
// The view keeps the display on while the Mac is unlocked, as the Mac's own
// screen does, and says the lock at once and when it changes.
func TestAWindowViewKeepsTheDisplayOnAndSaysTheLock(t *testing.T) {
	fake := NewFakeBackend(64, 48)
	backend := lockingViewer{fake, &fakeWindowViewer{pid: 4242, window: AppWindow{ID: 77, Bounds: testCell}}}
	s := NewServiceWith(t.TempDir(), backend)
	s.lockPoll = 10 * time.Millisecond
	a := s.AgentWindows()
	if a == nil {
		t.Fatal("no agent windows on a backend with an agent display")
	}
	t.Cleanup(a.Close)
	a.Opened("c1-1", "mh-signin", testCell)
	view, err := a.View("mh-signin")
	if err != nil {
		t.Fatal(err)
	}
	held := func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.DisplayHeld
	}
	if !held() {
		t.Fatal("the display is not kept on while the person views the window")
	}
	var mu sync.Mutex
	var heard []bool
	view.Locked(func(locked bool) {
		mu.Lock()
		heard = append(heard, locked)
		mu.Unlock()
	})
	fake.mu.Lock()
	fake.Locked = true
	fake.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := append([]bool(nil), heard...)
		mu.Unlock()
		if reflect.DeepEqual(got, []bool{false, true}) && !held() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heard %v, display held %v; want [false true], let go while locked", got, held())
		}
		time.Sleep(10 * time.Millisecond)
	}
	fake.mu.Lock()
	fake.Locked = false
	fake.mu.Unlock()
	for !held() {
		if time.Now().After(deadline.Add(2 * time.Second)) {
			t.Fatal("the display was not kept on again once unlocked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	view.Close()
	for held() {
		if time.Now().After(deadline.Add(4 * time.Second)) {
			t.Fatal("the display stayed held after the view closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
