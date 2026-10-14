package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeGitHub struct {
	t         *testing.T
	mu        sync.Mutex
	responses []map[string]any
	tokens    int
	userCalls int
}

func (f *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			f.t.Errorf("device code Accept = %q", r.Header.Get("Accept"))
		}
		if err := r.ParseForm(); err != nil {
			f.t.Error(err)
		}
		if r.PostForm.Get("client_id") != "client-123" || r.PostForm.Get("scope") != "read:user" {
			f.t.Errorf("device code form = %v", r.PostForm)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"device_code":      "device-secret",
			"user_code":        "WDJB-MJHT",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			f.t.Errorf("token Accept = %q", r.Header.Get("Accept"))
		}
		if err := r.ParseForm(); err != nil {
			f.t.Error(err)
		}
		if r.PostForm.Get("client_id") != "client-123" ||
			r.PostForm.Get("device_code") != "device-secret" ||
			r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			f.t.Errorf("token form = %v", r.PostForm)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokens++
		response := f.responses[0]
		f.responses = f.responses[1:]
		// GitHub reports device flow errors with HTTP 200.
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.userCalls++
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer gho_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id": 9007199254740993, "login": "octocat"}`))
	})
	return mux
}

func newGitHubTest(t *testing.T, responses ...map[string]any) (*GitHubProvider, *fakeGitHub) {
	fake := &fakeGitHub{t: t, responses: responses}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	now := time.UnixMilli(1_800_000_000_000)
	return &GitHubProvider{
		ClientID: "client-123",
		BaseURL:  server.URL,
		APIURL:   server.URL,
		Client:   server.Client(),
		Now:      func() time.Time { return now },
	}, fake
}

func TestGitHubDeviceShowsServiceURI(t *testing.T) {
	provider, _ := newGitHubTest(t)
	provider.VerificationURI = "https://connect.example/device"
	start, err := provider.StartDevice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if start.VerificationURI != "https://connect.example/device" || start.VerificationURIComplete != "" || start.UserCode != "WDJB-MJHT" {
		t.Fatalf("start = %+v", start)
	}
}

func TestGitHubDeviceFlow(t *testing.T) {
	provider, fake := newGitHubTest(t,
		map[string]any{"error": "authorization_pending"},
		map[string]any{"error": "slow_down", "interval": 10},
		map[string]any{"access_token": "gho_token", "token_type": "bearer", "scope": "read:user"},
	)
	ctx := context.Background()
	start, err := provider.StartDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if start.Handle != "device-secret" || start.UserCode != "WDJB-MJHT" ||
		start.VerificationURI != "https://github.com/login/device" || start.Interval != 5*time.Second ||
		!start.ExpiresAt.Equal(provider.Now().Add(15*time.Minute)) {
		t.Fatalf("start = %+v", start)
	}
	if result, err := provider.Poll(ctx, start.Handle); err != nil || result.Status != PollPending || result.SlowDown {
		t.Fatalf("pending = %+v %v", result, err)
	}
	if result, err := provider.Poll(ctx, start.Handle); err != nil || result.Status != PollPending ||
		!result.SlowDown || result.Interval != 10*time.Second {
		t.Fatalf("slow_down = %+v %v", result, err)
	}
	result, err := provider.Poll(ctx, start.Handle)
	if err != nil || result.Status != PollComplete ||
		result.Subject != (Subject{ID: "9007199254740993", Login: "octocat"}) {
		t.Fatalf("complete = %+v %v", result, err)
	}
	if fake.tokens != 3 || fake.userCalls != 1 {
		t.Fatalf("calls tokens=%d user=%d", fake.tokens, fake.userCalls)
	}
}

func TestGitHubDeviceFlowTerminalErrors(t *testing.T) {
	provider, _ := newGitHubTest(t,
		map[string]any{"error": "access_denied"},
		map[string]any{"error": "expired_token"},
		map[string]any{"error": "incorrect_client_credentials", "error_description": "secret detail"},
	)
	ctx := context.Background()
	if result, err := provider.Poll(ctx, "device-secret"); err != nil || result.Status != PollDenied {
		t.Fatalf("denied = %+v %v", result, err)
	}
	if result, err := provider.Poll(ctx, "device-secret"); err != nil || result.Status != PollExpired {
		t.Fatalf("expired = %+v %v", result, err)
	}
	_, err := provider.Poll(ctx, "device-secret")
	if err == nil || err.Error() != "github access token: incorrect_client_credentials" {
		t.Fatalf("unknown error = %v", err)
	}
}

func TestGitHubProviderLoginThroughService(t *testing.T) {
	provider, _ := newGitHubTest(t,
		map[string]any{"error": "authorization_pending"},
		map[string]any{"access_token": "gho_token"},
	)
	h := newHarness(t, func(config *Config) {
		config.Identity = provider
		config.DeviceHandler = nil
	})
	provider.Now = h.clock.Now
	daemon := newDaemon(t)
	start := h.startLogin(daemon, "studio", newRouteID(t))
	if start.UserCode != "WDJB-MJHT" || start.IntervalSeconds != 5 {
		t.Fatalf("start = %+v", start)
	}
	h.clock.Advance(5 * time.Second)
	if status, response, _ := h.poll(daemon, start.LoginID); status != http.StatusAccepted || response.Status != "pending" {
		t.Fatalf("pending poll = %d %+v", status, response)
	}
	h.clock.Advance(5 * time.Second)
	status, response, raw := h.poll(daemon, start.LoginID)
	if status != http.StatusOK || response.Machine == nil ||
		response.Machine.Provider != "github" || response.Machine.AccountLogin != "octocat" {
		t.Fatalf("complete poll = %d %s", status, raw)
	}
	var handle string
	if err := h.store.db.QueryRow(`SELECT provider_handle FROM logins WHERE id = ?`, start.LoginID).Scan(&handle); err != nil {
		t.Fatal(err)
	}
	if handle != "" {
		t.Fatal("device code kept after login completed")
	}
	if _, err := getAccount(context.Background(), h.store.db, `subject = ?`, "github:9007199254740993"); err != nil {
		t.Fatal(err)
	}
}
