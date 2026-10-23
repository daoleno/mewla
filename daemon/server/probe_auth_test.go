package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
)

func TestProbeAuthCheckAndWebSocketBoundaryRequireIndependentNonces(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, nil, nil, nil, nil, nil, nil)

	authCheckHeader := probeAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	websocketHeader := probeAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	if authCheckHeader == websocketHeader {
		t.Fatal("fresh probe authorizations unexpectedly matched")
	}

	authCheck := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	authCheck.Header.Set("Authorization", authCheckHeader)
	authCheckResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authCheckResponse, authCheck)
	if authCheckResponse.Code != http.StatusOK {
		t.Fatalf(
			"auth-check status=%d body=%q",
			authCheckResponse.Code,
			authCheckResponse.Body.String(),
		)
	}

	websocketProbe := httptest.NewRequest(http.MethodGet, "/ws", nil)
	websocketProbe.Header.Set("Authorization", websocketHeader)
	websocketResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(websocketResponse, websocketProbe)
	if websocketResponse.Code != http.StatusOK {
		t.Fatalf(
			"websocket probe status=%d body=%q",
			websocketResponse.Code,
			websocketResponse.Body.String(),
		)
	}

	replay := httptest.NewRequest(http.MethodGet, "/ws", nil)
	replay.Header.Set("Authorization", authCheckHeader)
	replayResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"replayed probe status=%d body=%q",
			replayResponse.Code,
			replayResponse.Body.String(),
		)
	}
}

func probeAuthorizationHeader(
	t *testing.T,
	privateKey ed25519.PrivateKey,
	daemonID string,
	deviceID string,
) string {
	t.Helper()
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonceHex := hex.EncodeToString(nonce)
	signature := ed25519.Sign(
		privateKey,
		auth.BuildSignaturePayload(
			"mewla-probe",
			daemonID,
			deviceID,
			timestamp,
			nonceHex,
		),
	)
	return auth.AuthorizationHeaderPrefix +
		"v1:" +
		deviceID +
		":" +
		daemonID +
		":" +
		timestamp +
		":" +
		nonceHex +
		":" +
		hex.EncodeToString(signature)
}

func TestAuthCheckReportsComputerName(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, nil, nil, nil, nil, nil, nil)
	server.SetComputerName("daoleno's MacBook Pro")

	request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	request.Header.Set("Authorization", probeAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("auth-check status=%d body=%q", response.Code, response.Body.String())
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "daoleno's MacBook Pro" {
		t.Fatalf("name = %q", body.Name)
	}
}

// A signed request proves a device, not that its Host is one of this
// computer's addresses: a tunnel's Host or a forged X-Forwarded-Proto must
// never add an entry to the address book.
func TestSignedRequestsNeverTeachTheAddressBook(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	book, err := addressbook.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(manager, nil, nil, nil, nil, nil, nil)
	server.SetAddressBook(book)

	request := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	request.Host = "tunnel.example.com"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Authorization", probeAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("auth-check status=%d body=%q", response.Code, response.Body.String())
	}
	entries, err := book.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("address book learned %+v from a request", entries)
	}
}
