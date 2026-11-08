package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/daoleno/mewla/daemon/browser"
)

type quietBackend struct{}

func (quietBackend) Capability(context.Context) browser.Capability {
	return browser.Capability{Available: true}
}
func (quietBackend) Start(context.Context, string) (browser.Host, error) {
	return nil, browser.ErrUnavailable
}

// The only thing the app asks of /browser is the Virtual screen switch; the
// browser itself is a window on the agent screen, listed and shown by /display.
func TestBrowserRouteIsOnlyTheVirtualScreenSwitch(t *testing.T) {
	a, priv, device := sessionFileAuthFixture(t)
	m, err := browser.New(t.TempDir(), quietBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetBrowser(m)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	defer s.shutdownAuthenticatedClients()
	request := func(body string, signed bool) (browser.Response, int) {
		req, _ := http.NewRequest("POST", h.URL+"/browser", bytes.NewReader([]byte(body)))
		if signed {
			req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, "mewla-browser"))
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
	if _, status := request(`{"action":"settings"}`, false); status == 200 {
		t.Fatal("an unauthenticated request was answered")
	}
	for _, body := range []string{`{"action":"list"}`, `{"action":"control","id":"x"}`, `{"action":"create"}`, `{"action":"begin"}`} {
		if _, status := request(body, true); status == 200 {
			t.Fatalf("%s was answered", body)
		}
	}
	got, status := request(`{"action":"settings"}`, true)
	if status != 200 || got.VirtualScreen == nil || *got.VirtualScreen {
		t.Fatalf("settings: %d %+v", status, got)
	}
	got, status = request(`{"action":"settings","enabled":true}`, true)
	if status != 200 || got.VirtualScreen == nil || !*got.VirtualScreen || !m.VirtualScreen() {
		t.Fatalf("switch on: %d %+v", status, got)
	}
}
