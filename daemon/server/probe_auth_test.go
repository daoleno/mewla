package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/gorilla/websocket"
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

// The browser WebSocket API cannot set headers, so a browser carries its
// device authorization in the one subprotocol it offers. The query string
// carries no credential.
func TestBrowserWebSocketAuthenticatesThroughASubprotocol(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, nil, nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	subprotocol := func(header string) string {
		return authSubprotocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(header))
	}
	connectHeader := func() string {
		return calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "mewla-connect")
	}

	header := connectHeader()
	dialer := websocket.Dialer{Subprotocols: []string{subprotocol(header)}}
	conn, response, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("subprotocol auth was refused: %v", err)
	}
	if got := conn.Subprotocol(); got != subprotocol(header) {
		t.Fatalf("selected subprotocol = %q, a browser needs the one it offered", got)
	}
	_ = conn.Close()
	_ = response

	// Single use: the same signed header cannot open a second socket.
	if conn, _, err := dialer.Dial(wsURL, nil); err == nil {
		_ = conn.Close()
		t.Fatal("replayed subprotocol authorization opened a second socket")
	}

	// A signature for another purpose is refused, as it is in a header.
	wrong := websocket.Dialer{Subprotocols: []string{subprotocol(calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "mewla-screen-view"))}}
	if conn, _, err := wrong.Dial(wsURL, nil); err == nil {
		_ = conn.Close()
		t.Fatal("a screen-view authorization opened /ws")
	}

	// The old ?auth= query credential is gone.
	query := wsURL + "?auth=" + base64.RawURLEncoding.EncodeToString([]byte(connectHeader()))
	if conn, resp, err := websocket.DefaultDialer.Dial(query, nil); err == nil {
		_ = conn.Close()
		t.Fatal("a query-string authorization opened /ws")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("query authorization: %v", err)
	}

	// A subprotocol is not an Authorization substitute on plain requests.
	plain := httptest.NewRequest(http.MethodGet, "/auth-check", nil)
	plain.Header.Set("Sec-WebSocket-Protocol", subprotocol(probeAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID)))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, plain)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("plain request with a subprotocol credential: %d", recorder.Code)
	}
}
