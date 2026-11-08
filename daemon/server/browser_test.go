package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/gorilla/websocket"
)

type viewerBackend struct{ runtime *viewerRuntime }

func (b viewerBackend) Capability(context.Context) browser.Capability {
	return browser.Capability{Available: true}
}
func (b viewerBackend) Start(context.Context, string) (browser.Host, error) {
	return viewerHost{b.runtime}, nil
}

type viewerHost struct{ runtime *viewerRuntime }

func (h viewerHost) Alive() bool                                     { return true }
func (h viewerHost) Close() error                                    { return nil }
func (h viewerHost) Window(context.Context) (browser.Runtime, error) { return h.runtime, nil }

type viewerRuntime struct {
	released atomic.Int32
	reads    atomic.Int32
	inputs   atomic.Int32
}

func (r *viewerRuntime) Target() string                { return "tab" }
func (r *viewerRuntime) Alive() bool                   { return true }
func (r *viewerRuntime) Close() error                  { return nil }
func (r *viewerRuntime) Release(context.Context) error { r.released.Add(1); return nil }
func (r *viewerRuntime) Command(_ context.Context, c browser.Command) (json.RawMessage, error) {
	if c.Kind == "input" {
		r.inputs.Add(1)
	}
	return json.RawMessage("{}"), nil
}
func (r *viewerRuntime) Stream(context.Context) (browser.Stream, error) {
	return &viewerStream{r: r, done: make(chan struct{})}, nil
}

type viewerStream struct {
	r    *viewerRuntime
	done chan struct{}
	once sync.Once
	seq  uint64
}

func (s *viewerStream) Read() ([]byte, error) {
	select {
	case <-s.done:
		return nil, io.EOF
	default:
	}
	s.r.reads.Add(1)
	s.seq++
	return json.Marshal(map[string]any{"type": "frame", "seq": s.seq, "data": "aGVsbG8=", "metadata": map[string]int{"deviceWidth": 1280, "deviceHeight": 800}})
}
func (s *viewerStream) Ack(uint64) error { return nil }
func (s *viewerStream) Close() error     { s.once.Do(func() { close(s.done) }); return nil }
func TestBrowserAuthenticatedViewerBackpressureReconnectAndRevocation(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	rt := &viewerRuntime{}
	m, err := browser.New(t.TempDir(), viewerBackend{rt})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetBrowser(m)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	defer s.shutdownAuthenticatedClients()
	request := func(q browser.Request, purpose string) (browser.Response, int) {
		raw, _ := json.Marshal(q)
		req, _ := http.NewRequest("POST", h.URL+"/browser", bytes.NewReader(raw))
		if purpose != "" {
			req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, purpose))
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var result browser.Response
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return result, resp.StatusCode
	}
	if _, status := request(browser.Request{Action: "list"}, ""); status == 200 {
		t.Fatal("unauthenticated list")
	}
	// A person cannot make or start browsers; an agent's first request for
	// control opens its window, which is what the viewer shows.
	if _, status := request(browser.Request{Action: "create"}, "mewla-browser"); status == 200 {
		t.Fatal("a person created a browser")
	}
	on := true
	if _, err := m.Handle(context.Background(), browser.Owner{Kind: "human", ID: "server-owner"}, browser.Request{Action: "settings", Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Handle(context.Background(), browser.Owner{Kind: "agent", ID: "worker/client"}, browser.Request{Action: "control"}); err != nil {
		t.Fatal(err)
	}
	listed, status := request(browser.Request{Action: "list"}, "mewla-browser")
	if status != 200 || len(listed.Resources) != 1 {
		t.Fatalf("list: %d %+v", status, listed.Resources)
	}
	id := listed.Resources[0].ID
	if _, status = request(browser.Request{Action: "control", ID: id}, "mewla-browser"); status == 200 {
		t.Fatal("HTTP bypassed viewer lease")
	}
	dial := func(purpose string) (*websocket.Conn, error) {
		header := http.Header{"Authorization": []string{calendarAuthHeader(priv, a.DaemonID(), device, purpose)}}
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/browser/viewer?id="+id, header)
		return c, err
	}
	if c, e := dial("mewla-browser-view:00000000-0000-0000-0000-000000000000"); e == nil {
		c.Close()
		t.Fatal("resource scope bypassed")
	}
	for attempt := 0; attempt < 2; attempt++ {
		c, e := dial("mewla-browser-view:" + id)
		if e != nil {
			t.Fatal(e)
		}
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		var frame struct {
			Type string
			Seq  uint64
		}
		if e = c.ReadJSON(&frame); e != nil || frame.Type != "frame" {
			t.Fatalf("frame: %v %+v", e, frame)
		}
		before := rt.reads.Load()
		time.Sleep(40 * time.Millisecond)
		if rt.reads.Load() != before {
			t.Fatal("unbounded frames without render ACK")
		}
		s.broadcastJSON(map[string]any{"type": "unrelated_worker_event"})
		c.WriteJSON(map[string]any{"type": "control", "request_id": "take"})
		var response struct {
			Type     string
			Response browser.Response
			Error    string
		}
		if e = c.ReadJSON(&response); e != nil || response.Response.Lease == nil {
			t.Fatalf("control starved behind unacked frame: %v %+v", e, response)
		}
		// The person's own input and Stop are viewer messages too: a touch reaches
		// the page, a halt answers with the resource.
		c.WriteJSON(map[string]any{"type": "touch", "command": map[string]any{"kind": "input", "input": map[string]any{"kind": "mousePressed", "x": 3, "y": 4, "button": "left"}}})
		c.WriteJSON(map[string]any{"type": "halt", "request_id": "halt1"})
		for {
			var any1 struct {
				Type      string
				RequestID string `json:"request_id"`
				Response  browser.Response
				Error     string
			}
			if e = c.ReadJSON(&any1); e != nil {
				t.Fatalf("halt response: %v", e)
			}
			if any1.Type == "response" && any1.RequestID == "halt1" {
				if any1.Error != "" || any1.Response.Resource == nil {
					t.Fatalf("halt: %+v", any1)
				}
				break
			}
		}
		if rt.inputs.Load() == 0 {
			t.Fatal("the person's touch did not reach the page")
		}
		c.WriteJSON(map[string]any{"type": "ack", "seq": frame.Seq})
		if e = c.ReadJSON(&frame); e != nil {
			t.Fatal(e)
		}
		c.WriteJSON(map[string]any{"type": "close"})
		_, _, e = c.ReadMessage()
		if !websocket.IsCloseError(e, 1000) {
			t.Fatalf("close handshake: %v", e)
		}
		c.Close()
		deadline := time.Now().Add(time.Second)
		for s.clientCount() != 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
	c, e := dial("mewla-browser-view:" + id)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var discard any
	c.ReadJSON(&discard)
	req := httptest.NewRequest("DELETE", "/devices", strings.NewReader(`{"device_id":"`+device+`"}`))
	req.Header.Set("Authorization", deviceAdminAuthorization(t, priv, a.DaemonID(), device, auth.DeviceRevokePurpose(device)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if _, _, e = c.ReadMessage(); e == nil {
		t.Fatal("revocation left viewer open")
	}
	if c, e := dial("mewla-browser-view:" + id); e == nil {
		c.Close()
		t.Fatal("revoked reconnect accepted")
	}
	if rt.released.Load() < 4 {
		t.Fatal("disconnect did not release held input")
	}
}
