package server

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/vdisplay"
)

// macAgentDisplayBackend is a scripted computer with an agent display, as a
// Mac's: its whole display is captured, and input reaches one app's window.
type macAgentDisplayBackend struct {
	*screen.FakeBackend
	session *windowCaptureSession
}

var macAgentDisplay = image.Rect(1512, 0, 3112, 1000)

func (b *macAgentDisplayBackend) ViewAgentDisplay(context.Context, string) (screen.Session, image.Rectangle, func(), error) {
	return b.session, macAgentDisplay, func() {}, nil
}

func (b *macAgentDisplayBackend) AgentWindowAt(rect image.Rectangle) (int, screen.AppWindow, bool) {
	return 4242, screen.AppWindow{ID: 77, Bounds: rect}, true
}

func (b *macAgentDisplayBackend) FocusWindow(int, screen.AppWindow) error { return nil }

func (b *macAgentDisplayBackend) WindowPlace(image.Rectangle) (image.Rectangle, bool) {
	return image.Rectangle{}, false
}

type windowCaptureSession struct {
	mu     sync.Mutex
	clicks []image.Point
	keys   []uint32
}

func (s *windowCaptureSession) Frame(ctx context.Context, after uint64) (screen.Frame, error) {
	if after > 0 {
		<-ctx.Done()
		return screen.Frame{}, ctx.Err()
	}
	return screen.Frame{Seq: 1, Image: image.NewRGBA(image.Rect(0, 0, macAgentDisplay.Dx(), macAgentDisplay.Dy()))}, nil
}
func (s *windowCaptureSession) Pointer(int, int, uint8) error { return nil }
func (s *windowCaptureSession) Key(uint32, bool) error        { return nil }
func (s *windowCaptureSession) Close() error                  { return nil }
func (s *windowCaptureSession) BackgroundClicks() (bool, string) {
	return true, ""
}
func (s *windowCaptureSession) ClickTo(_ int, _ screen.AppWindow, x, y, _, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clicks = append(s.clicks, image.Pt(x, y))
	return nil
}
func (s *windowCaptureSession) KeyTo(_ int, keysym uint32, down bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if down {
		s.keys = append(s.keys, keysym)
	}
	return nil
}

// On a Mac, /display is the agents' browser windows on the agent display, in
// the same contract as Linux's monitors: a Chrome-only Worker's window is
// listed under its name with its size and window count, its viewer shows that
// window's place and takes the person's click and keys there (told to the
// agent as counts), Hold and Stop refuse its browser calls, and its window
// closing takes it off the list.
func TestMacAgentWindowsAreServedAsDisplays(t *testing.T) {
	requireVideo(t)
	a, priv, device := sessionFileAuthFixture(t)
	capture := &windowCaptureSession{}
	fake := screen.NewFakeBackend(64, 64)
	fake.AgentDisplay = true
	backend := &macAgentDisplayBackend{FakeBackend: fake, session: capture}
	if screen.NewServiceWith(t.TempDir(), screen.NewFakeBackend(64, 64)).AgentWindows() != nil {
		t.Fatal("a computer without an agent display has agent windows")
	}
	windows := screen.NewServiceWith(t.TempDir(), backend).AgentWindows()
	if windows == nil {
		t.Fatal("a computer with an agent display has no agent windows")
	}
	defer windows.Close()
	windows.SetNames(func(string) string { return "SignIn" })
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetDisplays(windows)
	s.SetScreenLoopbackOnly(true)
	h := serveViewers(t, s.Handler())
	defer s.shutdownAuthenticatedClients()
	auth := func(purpose string) string { return calendarAuthHeader(priv, a.DaemonID(), device, purpose) }
	list := func() (out struct {
		Displays  []vdisplay.Info `json:"displays"`
		Available bool            `json:"available"`
		Reason    string          `json:"reason"`
	}) {
		req, _ := http.NewRequest("GET", h.URL+"/display", nil)
		req.Header.Set("Authorization", auth("mewla-display"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := list(); !got.Available || got.Reason != "" || len(got.Displays) != 0 {
		t.Fatalf("before any window: %+v", got)
	}
	windows.Opened("c1-1", "mh-signin", image.Rect(2312, 0, 3112, 500))
	got := list()
	if len(got.Displays) != 1 || got.Displays[0].ID != "mh-signin" || got.Displays[0].Name != "SignIn" || got.Displays[0].Browser != 1 || got.Displays[0].Width != 800 || got.Displays[0].Height != 500 {
		t.Fatalf("listed = %+v", got)
	}

	v := dialVideoViewer(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/display/viewer?worker=mh-signin", viewerIdentity(priv, a, device))
	v.next("ready")
	v.picture()
	var size struct{ Width, Height int }
	for size.Width == 0 {
		_ = json.Unmarshal(v.next("stats"), &size)
	}
	if size.Width != 800 || size.Height != 500 {
		t.Fatalf("the viewer is not shown the window's place: %dx%d", size.Width, size.Height)
	}
	v.send([]byte{5, 1, 0, 40, 0, 30})       // press
	v.send([]byte{5, 0, 0, 40, 0, 30})       // release
	v.send([]byte{4, 1, 0, 0, 0, 0, 0, 'q'}) // one key
	deadline := time.Now().Add(5 * time.Second)
	for {
		capture.mu.Lock()
		clicks, keys := append([]image.Point(nil), capture.clicks...), append([]uint32(nil), capture.keys...)
		capture.mu.Unlock()
		if len(clicks) == 1 && len(keys) == 1 {
			if clicks[0] != image.Pt(800+40, 30) || keys[0] != 'q' {
				t.Fatalf("input landed at %v keys %v, want the window's (40, 30) and q", clicks, keys)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the person's input never reached the window: %v %v", clicks, keys)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := windows.Begin("mh-signin"); err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(windows.End("mh-signin"), "\n")
	if !strings.Contains(notes, "1 click") || !strings.Contains(notes, "1 character typed (not shown to you)") || strings.Contains(notes, "q,") {
		t.Fatalf("the agent was told %q", notes)
	}

	// Hold, from the viewer, refuses the Worker's browser calls until let go.
	v.sendJSON(map[string]any{"type": "hold", "on": true})
	waitUntil(t, func() bool { return windows.Begin("mh-signin") != nil })
	if l := list(); l.Displays[0].Control != "person" {
		t.Fatalf("a held window: %+v", l.Displays)
	}
	v.sendJSON(map[string]any{"type": "hold", "on": false})
	waitUntil(t, func() bool {
		if windows.Begin("mh-signin") != nil {
			return false
		}
		windows.End("mh-signin")
		return true
	})

	// Stop, from the HTTP side, refuses them too.
	req, _ := http.NewRequest("POST", h.URL+"/display", strings.NewReader(`{"action":"halt","worker":"mh-signin"}`))
	req.Header.Set("Authorization", auth("mewla-display"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("halt = %d", resp.StatusCode)
	}
	if err := windows.Begin("mh-signin"); err == nil || !strings.Contains(err.Error(), "stopped you") {
		t.Fatalf("a call after Stop: %v", err)
	}

	// The window closing takes it off the list and ends the view.
	windows.Closed("c1-1")
	if l := list(); len(l.Displays) != 0 {
		t.Fatalf("a closed window is listed: %+v", l.Displays)
	}
	v.errorText()
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
