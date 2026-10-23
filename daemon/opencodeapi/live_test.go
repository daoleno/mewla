package opencodeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeService is an OpenCode service that pairs, takes calls with the paired
// token, and streams events.
type fakeService struct {
	server *httptest.Server
	pairs  atomic.Int32
	// valid is the token the service currently accepts.
	valid  atomic.Value
	events chan string
	prompt chan map[string]any
}

func newFakeService(t *testing.T) *fakeService {
	t.Helper()
	f := &fakeService{events: make(chan string, 8), prompt: make(chan map[string]any, 4)}
	f.valid.Store("")
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/connect/", func(w http.ResponseWriter, r *http.Request) {
		token := fmt.Sprintf("token-%d", f.pairs.Load())
		f.valid.Store(token)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "opencode" || pass != f.valid.Load().(string) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"_tag":"UnauthorizedError"}`)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api/session/active", authed(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"ses_a":{"type":"running"}}}`)
	}))
	mux.HandleFunc("/api/session/ses_gone", authed(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"_tag":"SessionNotFoundError","message":"Session not found"}`)
	}))
	mux.HandleFunc("/api/session/ses_a/prompt", authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.HasPrefix(fmt.Sprint(body["id"]), "msg_") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"_tag":"InvalidRequestError","message":"Expected a string starting with \"msg_\""}`)
			return
		}
		f.prompt <- body
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": body["id"]}})
	}))
	mux.HandleFunc("/api/event", authed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": heartbeat\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case e := <-f.events:
				_, _ = io.WriteString(w, "data: "+e+"\n\n")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}))
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func installPairing(t *testing.T, f *fakeService) {
	t.Helper()
	previous := Runner
	Runner = func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[1] {
		case "server.info":
			return []byte(fmt.Sprintf(`{"urls":[%q]}`, f.server.URL)), nil
		case "server.pair":
			f.pairs.Add(1)
			return []byte(`{"code":"one-time","expires_in":300}`), nil
		}
		return nil, fmt.Errorf("unexpected operation %v", args)
	}
	t.Cleanup(func() { Runner = previous })
}

func TestLivePairsThroughTheOfficialClientAndSendsWithTheToken(t *testing.T) {
	f := newFakeService(t)
	installPairing(t, f)
	live, err := OpenLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := live.Prompt(context.Background(), "ses_a", "msg_receipt1", "hello")
	if err != nil || receipt != "msg_receipt1" {
		t.Fatalf("receipt = %q, %v", receipt, err)
	}
	if got := <-f.prompt; got["text"] != "hello" || got["id"] != "msg_receipt1" {
		t.Fatalf("service got %v", got)
	}
	if _, err := live.Prompt(context.Background(), "ses_a", "mewla-1", "x"); !Rejected(err) {
		t.Fatalf("a refused id must be a rejection, got %v", err)
	}
}

func TestLivePairsAgainWhenTheTokenIsRefused(t *testing.T) {
	f := newFakeService(t)
	installPairing(t, f)
	live, err := OpenLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.valid.Store("rotated") // the service restarted
	active, err := live.Active(context.Background())
	if err != nil || !active["ses_a"] || f.pairs.Load() != 2 {
		t.Fatalf("active = %v, pairs = %d, err = %v", active, f.pairs.Load(), err)
	}
}

func TestLiveKnowsAMissingSession(t *testing.T) {
	f := newFakeService(t)
	installPairing(t, f)
	live, _ := OpenLive(context.Background())
	if ok, err := live.SessionExists(context.Background(), "ses_gone"); ok || err != nil {
		t.Fatalf("exists = %v, err = %v", ok, err)
	}
}

func TestLiveStreamDeliversEachEventAndEndsWhenTheServiceDoes(t *testing.T) {
	f := newFakeService(t)
	installPairing(t, f)
	live, _ := OpenLive(context.Background())
	got := make(chan string, 4)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- live.Stream(ctx, func(raw []byte) { got <- string(raw) }) }()
	f.events <- `{"type":"a"}`
	f.events <- `{"type":"b"}`
	for _, want := range []string{`{"type":"a"}`, `{"type":"b"}`} {
		select {
		case e := <-got:
			if e != want {
				t.Fatalf("event %q, want %q", e, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no event")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled stream reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not end")
	}
}
