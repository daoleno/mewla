package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
)

func boundConnectHeader(privateKey ed25519.PrivateKey, daemonID, deviceID, binding string) (string, string) {
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	nonce := hex.EncodeToString(random)
	signature := ed25519.Sign(privateKey, auth.BuildBoundSignaturePayload("mewla-connect", daemonID, deviceID, timestamp, nonce, binding))
	return auth.AuthorizationHeaderPrefix + "v2:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonce + ":" + base64.RawURLEncoding.EncodeToString([]byte(binding)) + ":" + hex.EncodeToString(signature), nonce
}

// Security review 11-23, PR B item 6: the first frame on a paired device's
// socket is the daemon key's answer to the nonce of the device's own connect,
// and a connect bound to another host (one the app was made to talk to, that
// handed the signature on) is refused.
func TestTheSocketOpensWithTheComputersProofAndRefusesAnotherHostsConnect(t *testing.T) {
	manager, privateKey := enrolledTestDevice(t)
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	defer srv.shutdownAuthenticatedClients()
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(host.Close)
	url := "ws" + strings.TrimPrefix(host.URL, "http")

	header, nonce := boundConnectHeader(privateKey, manager.DaemonID(), "device-direct", "host:127.0.0.1")
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {header}})
	if err != nil {
		t.Fatalf("a connect bound to the host it reached: %v", err)
	}
	defer conn.Close()
	var first map[string]any
	if err := conn.ReadJSON(&first); err != nil {
		t.Fatal(err)
	}
	text := func(key string) string { value, _ := first[key].(string); return value }
	proof := auth.ServerAssertion{Timestamp: text("timestamp"), NonceHex: text("nonce"), SignatureHex: text("signature")}
	if first["type"] != "daemon_proof" || text("purpose") != string(auth.ConnectProofPurpose) || !auth.VerifyChallengeAssertionFor(manager.PublicKeyHex(), auth.ConnectProofPurpose, nonce, proof) {
		t.Fatalf("first frame = %v", first)
	}

	forwarded, _ := boundConnectHeader(privateKey, manager.DaemonID(), "device-direct", "host:evil.example")
	if _, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {forwarded}}); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a connect made for evil.example was taken: %v", err)
	}
}

func TestRequestBinding(t *testing.T) {
	srv := New(nil, nil, nil, nil, nil, nil, nil)
	pin := strings.Repeat("ab", 32)
	srv.SetTransportPin(strings.ToUpper(pin))
	request := func(host, remote, sni, forwarded string, overTLS bool) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Host, r.RemoteAddr = host, remote
		if forwarded != "" {
			r.Header.Set("X-Forwarded-Host", forwarded)
		}
		if overTLS {
			r.TLS = &tls.ConnectionState{ServerName: sni}
		} else {
			r.TLS = nil
		}
		return r
	}
	cases := []struct {
		name    string
		r       *http.Request
		binding string
		want    bool
	}{
		{"pinned, over TLS", request("127.0.0.1:41234", "10.0.0.9:5000", "quiet-otter.m.mewla.ai", "", true), "pin:" + pin, true},
		{"another pin", request("127.0.0.1:41234", "10.0.0.9:5000", "quiet-otter.m.mewla.ai", "", true), "pin:" + strings.Repeat("cd", 32), false},
		{"a pin over plain HTTP", request("127.0.0.1:41234", "10.0.0.9:5000", "", "", false), "pin:" + pin, false},
		{"hosted, Host and SNI agree", request("quiet-otter.m.mewla.ai", "10.0.0.9:5000", "quiet-otter.m.mewla.ai", "", true), "host:quiet-otter.m.mewla.ai", true},
		{"through the relay with the Host of the host in the middle", request("evil.example", "10.0.0.9:5000", "quiet-otter.m.mewla.ai", "", true), "host:evil.example", false},
		{"plain LAN by address", request("192.168.1.20:9876", "192.168.1.30:5000", "", "", false), "host:192.168.1.20", true},
		{"IPv6 in brackets", request("[fd7a:115c::1]:9876", "[fd7a:115c::2]:5000", "", "", false), "host:fd7a:115c::1", true},
		{"another host", request("192.168.1.20:9876", "192.168.1.30:5000", "", "", false), "host:evil.example", false},
		{"a proxy on this computer that rewrites Host", request("127.0.0.1:9876", "127.0.0.1:5000", "", "Desk.Example", false), "host:desk.example", true},
		{"X-Forwarded-Host from elsewhere", request("192.168.1.20:9876", "192.168.1.30:5000", "", "desk.example", false), "host:desk.example", false},
		{"X-Forwarded-Host over TLS", request("quiet-otter.m.mewla.ai", "127.0.0.1:5000", "quiet-otter.m.mewla.ai", "evil.example", true), "host:evil.example", false},
		{"no prefix", request("desk.example", "192.168.1.30:5000", "", "", false), "desk.example", false},
	}
	for _, c := range cases {
		if got := srv.requestBinding(c.r)(c.binding); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// bindingHostname is the app's bindingHost line for line (client/core/deviceAuthContract.test.ts).
	for in, want := range map[string]string{"Desk.Example:443": "desk.example", "[FD7A:115C::1]:9876": "fd7a:115c::1", "desk.example.": "desk.example", "192.168.1.20": "192.168.1.20"} {
		if got := bindingHostname(in); got != want {
			t.Errorf("bindingHostname(%q) = %q, want %q", in, got, want)
		}
	}
}
