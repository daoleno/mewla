package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

func newHealthServer(t *testing.T) (*Server, *auth.Manager) {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil), authManager
}

func getHealth(t *testing.T, srv *Server, query string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/health"+query, nil)
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	body, _ := io.ReadAll(recorder.Result().Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	return recorder.Code, payload
}

func TestHealthChallengeIsSignedOverTheCallersChallenge(t *testing.T) {
	srv, authManager := newHealthServer(t)
	challenge := "00112233445566778899aabbccddeeff"

	status, payload := getHealth(t, srv, "?challenge="+challenge)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	answer := auth.ServerAssertion{
		Timestamp:    payload["assertion_timestamp"].(string),
		NonceHex:     payload["assertion_nonce"].(string),
		SignatureHex: payload["assertion_signature"].(string),
	}
	if payload["assertion_purpose"] != auth.HealthChallengePurpose {
		t.Fatalf("purpose = %v", payload["assertion_purpose"])
	}
	if !auth.VerifyChallengeAssertion(authManager.PublicKeyHex(), challenge, answer) {
		t.Fatal("the answer must verify for the challenge that was sent")
	}
	// The same recorded answer is no proof against a new challenge.
	if auth.VerifyChallengeAssertion(authManager.PublicKeyHex(), "ffeeddccbbaa99887766554433221100", answer) {
		t.Fatal("a replayed answer verified against a different challenge")
	}
}

func TestHealthChallengeRefusesMalformedChallenges(t *testing.T) {
	srv, _ := newHealthServer(t)
	for _, query := range []string{
		"?challenge=",
		"?challenge=zz",
		"?challenge=0011",
		"?challenge=00112233445566778899aabbccddeeff00",
		"?challenge=00112233445566778899aabbccddeeff&challenge=00112233445566778899aabbccddeeff",
	} {
		if status, _ := getHealth(t, srv, query); status != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", query, status)
		}
	}
}

func TestHealthWithoutChallengeKeepsItsPurpose(t *testing.T) {
	srv, _ := newHealthServer(t)
	status, payload := getHealth(t, srv, "")
	if status != http.StatusOK || payload["assertion_purpose"] != "mewla-health" {
		t.Fatalf("status=%d purpose=%v", status, payload["assertion_purpose"])
	}
}

func enrolledTestDevice(t *testing.T) (*auth.Manager, ed25519.PrivateKey) {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-direct", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	return authManager, privateKey
}

func TestLoopbackDaemonTellsThePhoneToForgetDirectAddresses(t *testing.T) {
	authManager, privateKey := enrolledTestDevice(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetDirectEndpoints(func() []DirectEndpoint { return nil })
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-direct", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for message := range messageSink(t, conn) {
		if message["type"] != "direct_addresses" {
			continue
		}
		addresses, ok := message["addresses"].([]any)
		if !ok || len(addresses) != 0 {
			t.Fatalf("addresses = %v, want an empty list", message["addresses"])
		}
		return
	}
	t.Fatal("no direct_addresses message")
}

func TestDirectAddressesComeFromTheLiveSource(t *testing.T) {
	authManager, privateKey := enrolledTestDevice(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	calls := 0
	srv.SetDirectEndpoints(func() []DirectEndpoint {
		calls++
		return []DirectEndpoint{{Host: "192.168.1.20", Port: 9876, Kind: "lan"}, {Host: "100.64.0.9", Port: 9876, Kind: "tailscale"}}
	})
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-direct", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for message := range messageSink(t, conn) {
		if message["type"] != "direct_addresses" {
			continue
		}
		encoded, _ := json.Marshal(message["addresses"])
		var got []DirectEndpoint
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		want := []DirectEndpoint{{Host: "192.168.1.20", Port: 9876, Kind: "lan"}, {Host: "100.64.0.9", Port: 9876, Kind: "tailscale"}}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("addresses = %v", got)
		}
		if calls != 1 {
			t.Fatalf("source read %d times, want once per connect", calls)
		}
		return
	}
	t.Fatal("no direct_addresses message")
}
