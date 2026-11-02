package connections

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestForeignOAuthCallbackPreservesOwnerAndDoesNotLeak(t *testing.T) {
	m := testManager(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("foreign callback reached provider")
		return nil, nil
	})
	m.pending["owned"] = &oauthFlow{ID: "own-account", Expires: time.Now().Add(time.Hour)}
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	for _, web := range []bool{false, true} {
		w := httptest.NewRecorder()
		m.BrowserOAuthCallback(w, httptest.NewRequest("GET", "http://127.0.0.1:9876/plugins/oauth/callback?state=another-computer&code=secret-code%3Cscript%3E", nil), web)
		if w.Code != 400 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "This sign-in belongs to another Mewla computer") || !strings.Contains(w.Body.String(), "window.location.href") || !strings.Contains(w.Body.String(), ">Copy</button>") {
			t.Fatalf("foreign return not recoverable: %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "secret-code") || strings.Contains(logs.String(), "secret-code") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'nonce-") {
			t.Fatal("callback secrecy boundary lost")
		}
		if len(m.pending) != 1 || m.pending["owned"] == nil || len(m.records) != 0 {
			t.Fatal("foreign return changed local ownership")
		}
	}
	w := httptest.NewRecorder()
	m.BrowserOAuthCallback(w, httptest.NewRequest("GET", "http://127.0.0.1:9876/plugins/oauth/callback?state=owned&state=other&code=x", nil), true)
	if w.Code != 400 || m.pending["owned"] == nil {
		t.Fatal("ambiguous state consumed owned flow")
	}
}
