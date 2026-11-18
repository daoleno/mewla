package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screen"
)

// The computer's own screen as pushed video: the socket carries the offer,
// the answer and the candidates; the picture comes as H.264 over WebRTC and a
// press on the input channel reaches the screen.
func TestScreenViewerAsVideo(t *testing.T) {
	requireVideo(t)
	a, priv, device := sessionFileAuthFixture(t)
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(t.TempDir(), fake)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	if err := service.Enable(context.Background(), device, "Pixel"); err != nil {
		t.Fatal(err)
	}
	pressed := make(chan struct{}, 1)
	fake.AfterPointer = func(int) {
		select {
		case pressed <- struct{}{}:
		default:
		}
	}
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-screen-view"))
	v := dialVideoViewer(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer", viewerIdentity(priv, a, device))
	v.next("ready")
	v.picture()
	v.send([]byte{5, 1, 0, 10, 0, 20}) // press at (10, 20)
	select {
	case <-pressed:
	case <-time.After(5 * time.Second):
		t.Fatal("the press never reached the screen")
	}
	if pointers, _ := fake.Snapshot(); len(pointers) == 0 || pointers[0] != (screen.FakePointer{X: 10, Y: 20, Buttons: 1}) {
		t.Fatalf("pointers %+v", pointers)
	}
}

// An answer whose DTLS fingerprints the paired device did not sign is never
// used: the session ends before any media flows.
func TestScreenVideoNeedsTheDevicesSignedAnswer(t *testing.T) {
	requireVideo(t)
	a, priv, device := sessionFileAuthFixture(t)
	fake := screen.NewFakeBackend(320, 200)
	service := screen.NewServiceWith(t.TempDir(), fake)
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	if err := service.Enable(context.Background(), device, "Pixel"); err != nil {
		t.Fatal(err)
	}
	id := viewerIdentity(priv, a, device)
	honest := id.sign
	id.sign = func(purpose string) string {
		if strings.HasPrefix(purpose, "mewla-screen-dtls:") {
			return honest("mewla-screen-dtls:sha256=" + strings.Repeat("0", 64)) // another certificate's
		}
		return honest(purpose)
	}
	v := dialVideoViewer(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer", id)
	if got := v.errorText(); !strings.Contains(got, "lost") {
		t.Fatalf("the computer said %q", got)
	}
	select {
	case <-v.frames:
		t.Fatal("a picture arrived for an unsigned answer")
	default:
	}
}

// A revoked device loses the view within seconds even when it sends nothing
// on the socket (its input rides the data channels, which carry no trust).
func TestARevokedSilentViewerIsCutOff(t *testing.T) {
	requireVideo(t)
	a, priv, device := sessionFileAuthFixture(t)
	service := screen.NewServiceWith(t.TempDir(), screen.NewFakeBackend(320, 200))
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetScreen(service)
	s.SetScreenLoopbackOnly(true)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	if err := service.Enable(context.Background(), device, "Pixel"); err != nil {
		t.Fatal(err)
	}
	v := dialVideoViewer(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/screen/viewer", viewerIdentity(priv, a, device))
	v.next("ready")
	v.picture()
	if _, err := a.RevokeDevice(device); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = v.closedWith()
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("cut off after %v", took)
	}
}
