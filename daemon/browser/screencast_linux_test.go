//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeChrome serves the page endpoints of a Chrome: on Page.startScreencast it
// sends a frame, and sends the next one only once the previous is acknowledged,
// as Chrome does. It counts the acknowledgements per page.
type fakeChrome struct {
	mu    sync.Mutex
	acks  map[string]int
	sent  map[string]int
	index int
}

func (f *fakeChrome) handler() http.Handler {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := strings.TrimPrefix(r.URL.Path, "/devtools/page/")
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		frame := func(n int) {
			f.mu.Lock()
			f.sent[page]++
			f.mu.Unlock()
			_ = c.WriteJSON(map[string]any{"method": "Page.screencastFrame", "params": map[string]any{
				"data": "aGVsbG8=", "sessionId": n, "metadata": map[string]any{"deviceWidth": 1280, "deviceHeight": 715, "pageUrl": page}}})
		}
		n := 0
		for {
			var msg struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if c.ReadJSON(&msg) != nil {
				return
			}
			switch msg.Method {
			case "Page.startScreencast":
				n++
				frame(n)
			case "Page.screencastFrameAck":
				f.mu.Lock()
				f.acks[page]++
				f.mu.Unlock()
				n++
				frame(n)
			}
		}
	})
}

func fakeWindow(t *testing.T, target string) (*linuxWindow, *fakeChrome) {
	t.Helper()
	f := &fakeChrome{acks: map[string]int{}, sent: map[string]int{}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	h := &linuxHost{endpoint: srv.URL, chrome: &child{done: make(chan struct{})}, display: &child{done: make(chan struct{})}}
	return &linuxWindow{h: h, target: target, keys: map[string]bool{}, buttons: map[string]bool{}}, f
}

func nextFrame(t *testing.T, s Stream) (uint64, int) {
	t.Helper()
	got := make(chan []byte, 1)
	go func() { raw, _ := s.Read(); got <- raw }()
	select {
	case raw := <-got:
		var frame struct {
			Type     string `json:"type"`
			Seq      uint64 `json:"seq"`
			Metadata struct {
				DeviceHeight int `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &frame) != nil || frame.Type != "frame" {
			t.Fatalf("not a frame: %s", raw)
		}
		return frame.Seq, frame.Metadata.DeviceHeight
	case <-time.After(3 * time.Second):
		t.Fatal("no frame")
		return 0, 0
	}
}

// The view is paced by the viewer's acknowledgements, keeps the frame's
// metadata (the viewer maps taps by it) and follows the window to another page.
func TestScreencastIsPacedByAcksAndFollowsThePage(t *testing.T) {
	w, chrome := fakeWindow(t, "tab-1")
	s, err := w.Stream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seq, height := nextFrame(t, s)
	if seq != 1 || height != 715 {
		t.Fatalf("first frame seq %d height %d", seq, height)
	}
	time.Sleep(60 * time.Millisecond)
	chrome.mu.Lock()
	if chrome.sent["tab-1"] != 1 {
		chrome.mu.Unlock()
		t.Fatalf("Chrome sent %d frames before the first was acknowledged", chrome.sent["tab-1"])
	}
	chrome.mu.Unlock()
	if err := s.Ack(seq); err != nil {
		t.Fatal(err)
	}
	if seq2, _ := nextFrame(t, s); seq2 != 2 {
		t.Fatalf("second frame seq %d", seq2)
	}
	// The window shows another page: the view moves to it, its frames number on.
	w.SetTarget("tab-2")
	var seq3 uint64
	for i := 0; i < 5; i++ {
		seq3, _ = nextFrame(t, s)
		chrome.mu.Lock()
		moved := chrome.sent["tab-2"] > 0
		chrome.mu.Unlock()
		if moved {
			break
		}
		_ = s.Ack(seq3)
	}
	chrome.mu.Lock()
	moved := chrome.sent["tab-2"] > 0
	chrome.mu.Unlock()
	if !moved || seq3 <= 2 {
		t.Fatalf("the view did not move to the new page (seq %d)", seq3)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil {
		t.Fatal("a closed view still reads")
	}
}
