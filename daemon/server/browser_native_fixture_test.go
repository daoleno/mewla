//go:build linux && browserfixture

package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/browser"
)

func TestBrowserNativeFixture(t *testing.T) {
	root := os.Getenv("ZEN_BROWSER_NATIVE_FIXTURE")
	if root == "" {
		t.Skip("owned Android fixture only")
	}
	a, e := auth.NewManager(filepath.Join(root, "auth"))
	if e != nil {
		t.Fatal(e)
	}
	backend := browser.NewFixtureBackend()
	if os.Getenv("ZEN_BROWSER_SECURE") == "1" {
		backend = browser.NewBackend()
	}
	m, e := browser.New(filepath.Join(root, "browsers"), backend)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetBrowser(m)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Method == "POST" {
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "yes", Path: "/", MaxAge: 3600})
			http.Redirect(w, r, "/", 303)
			return
		}
		_, e := r.Cookie("fixture")
		if e == nil {
			fmt.Fprint(w, `<style>body{font:32px sans-serif;background:#e9f3eb;padding:32px}</style><h1>Signed in on the host</h1><p>Browser-first fixture</p><button onclick="window.open('/popup')">Open another tab</button>`)
			return
		}
		fmt.Fprint(w, `<style>body{font:28px sans-serif;background:#f4f2ee;padding:32px}button{font:24px;padding:18px}</style><h1>Your persistent Browser</h1><p>This local fixture has no real account.</p><form method="post"><button>Sign in locally</button></form>`)
	}))
	defer site.Close()
	created, e := m.Handle(context.Background(), browser.Owner{Kind: "human", ID: "fixture"}, browser.Request{Action: "create", Name: "Research"})
	if e != nil {
		t.Fatal(e)
	}
	id := created.Resource.ID
	_, e = m.Handle(context.Background(), browser.Owner{Kind: "human", ID: "fixture"}, browser.Request{Action: "start", ID: id})
	if e != nil {
		t.Fatal(e)
	}
	ctl, e := m.Handle(context.Background(), browser.Owner{Kind: "human", ID: "fixture"}, browser.Request{Action: "control", ID: id})
	if e != nil {
		t.Fatal(e)
	}
	_, e = m.Handle(context.Background(), browser.Owner{Kind: "human", ID: "fixture"}, browser.Request{Action: "command", ID: id, Lease: ctl.Lease, Command: &browser.Command{Kind: "navigate", URL: site.URL}})
	if e != nil {
		t.Fatal(e)
	}
	m.ReleaseOwner(browser.Owner{Kind: "human", ID: "fixture"})
	h := httptest.NewUnstartedServer(s.Handler())
	h.Listener.Close()
	h.Listener, e = net.Listen("tcp", "127.0.0.1:19883")
	if e != nil {
		t.Fatal(e)
	}
	h.StartTLS()
	defer h.Close()
	defer s.shutdownAuthenticatedClients()
	// Current production Link envelope and native SPKI-pinned TLS path.
	link, e := mintFixtureLink(a, h.Certificate(), "https://10.0.2.2:19883")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "pairing-link"), []byte(link), 0600); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "ready"), []byte(id), 0600)
	t.Log("Isolated Browser fixture ready")
	deadline := time.NewTimer(12 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			return
		case <-tick.C:
			if _, e := os.Stat(filepath.Join(root, "stop")); e == nil {
				return
			}
		}
	}
}
