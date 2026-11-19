package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/screencast"
	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/gorilla/websocket"
)

// An app from before the video (it sends no video=webrtc) is told to update,
// in what it already shows: the status reason under "Screen can't start",
// and a viewer socket's error and close reason.
func TestAnOldAppIsToldToUpdate(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(t.TempDir(), fake)
	displays := vdisplay.NewService("")
	defer displays.Close()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetDisplays(displays)
	s.SetScreenLoopbackOnly(true)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	if err := service.Enable(context.Background(), device, "Pixel"); err != nil {
		t.Fatal(err)
	}
	auth := func(purpose string) string { return calendarAuthHeader(priv, a.DaemonID(), device, purpose) }

	status := func(query string) screen.Status {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, h.URL+"/screen"+query, nil)
		req.Header.Set("Authorization", auth("mewla-screen"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var st screen.Status
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	if st := status("?video=webrtc"); !st.Supported || !st.Ready || st.Reason != "" || st.Video != "webrtc" {
		t.Fatalf("a current app: %+v", st)
	}
	// The display list says so too: a current app opens no viewer of a computer that does not.
	req, _ := http.NewRequest(http.MethodGet, h.URL+"/display", nil)
	req.Header.Set("Authorization", auth("mewla-display"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct{ Video string }
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if listed.Video != "webrtc" {
		t.Fatalf("display list video = %q", listed.Video)
	}
	if st := status(""); st.Supported || st.Ready || st.Reason != oldAppScreen {
		t.Fatalf("an old app: %+v", st)
	}

	for _, view := range []struct{ path, purpose string }{
		{"/screen/viewer", "mewla-screen-view"},
		{"/screen/viewer?watch=1", "mewla-screen-view"},
		{"/display/viewer?worker=w1", "mewla-display-view"},
	} {
		header := http.Header{}
		header.Set("Authorization", auth(view.purpose))
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+view.path, header)
		if err != nil {
			t.Fatalf("%s: %v", view.path, err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		kind, data, err := conn.ReadMessage()
		if err != nil || kind != websocket.TextMessage {
			t.Fatalf("%s: first message %d %q %v", view.path, kind, data, err)
		}
		var msg struct{ Type, Error string }
		if _ = json.Unmarshal(data, &msg); msg.Type != "error" || msg.Error != oldAppScreen {
			t.Fatalf("%s: %s", view.path, data)
		}
		_, _, err = conn.ReadMessage()
		var closed *websocket.CloseError
		if !errors.As(err, &closed) || closed.Text != oldAppScreen {
			t.Fatalf("%s: closed with %v", view.path, err)
		}
		conn.Close()
	}
}

// A Linux computer without ffmpeg says so on Screen, before a display is
// opened; its agents keep their displays.
func TestTheDisplayListSaysFFmpegIsMissing(t *testing.T) {
	if err := vdisplay.Capability(); err != nil {
		t.Skip(err)
	}
	a, priv, device := sessionFileAuthFixture(t)
	displays := vdisplay.NewService("")
	defer displays.Close()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetDisplays(displays)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	list := func() (available bool, reason string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, h.URL+"/display", nil)
		req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-display"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var listed struct {
			Available bool
			Reason    string
		}
		if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
			t.Fatal(err)
		}
		return listed.Available, listed.Reason
	}
	s.videoReady = func() error { return screencast.ErrNoEncoder }
	if available, reason := list(); !available || reason != screencast.ErrNoEncoder.Error() {
		t.Fatalf("without ffmpeg: available %v, reason %q", available, reason)
	}
	s.videoReady = func() error { return nil }
	if available, reason := list(); !available || reason != "" {
		t.Fatalf("with ffmpeg: available %v, reason %q", available, reason)
	}
}
