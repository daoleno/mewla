package screen

import (
	"errors"
	"image"
	"reflect"
	"testing"
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
