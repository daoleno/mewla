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

	// Reached by a name (plain HTTP): a connect made for another name is refused. (By an address, plain HTTP proves
	// no name: TestRequestBinding.)
	forwarded, _ := boundConnectHeader(privateKey, manager.DaemonID(), "device-direct", "host:evil.example")
	if _, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {forwarded}, "Host": {"desk.local"}}); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
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
		{"hosted, the SNI names it", request("quiet-otter.m.mewla.ai", "10.0.0.9:5000", "quiet-otter.m.mewla.ai", "", true), "host:quiet-otter.m.mewla.ai", true},
		{"through the relay, made for the host in the middle", request("evil.example", "10.0.0.9:5000", "quiet-otter.m.mewla.ai", "", true), "host:evil.example", false},
		{"X-Forwarded-Host over TLS counts for nothing", request("quiet-otter.m.mewla.ai", "127.0.0.1:5000", "quiet-otter.m.mewla.ai", "evil.example", true), "host:evil.example", false},
		{"plain LAN by address", request("192.168.1.20:9876", "192.168.1.30:5000", "", "", false), "host:192.168.1.20", true},
		{"IPv6 in brackets", request("[fd7a:115c::1]:9876", "[fd7a:115c::2]:5000", "", "", false), "host:fd7a:115c::1", true},
		{"plain, a Host that is a name, made for another", request("desk.local:9876", "192.168.1.30:5000", "", "", false), "host:evil.example", false},
		// X-Forwarded-Host is never read (round 2 F1): behind a proxy that rewrites Host to its upstream address the
		// address case accepts any binding, whatever the header says (until 9055ad4b8 "passes another name" was refused
		// on the header).
		{"a proxy on this computer that passes the name it was asked for", request("127.0.0.1:9876", "127.0.0.1:5000", "", "Desk.Example", false), "host:desk.example", true},
		{"a proxy on this computer that rewrites Host to its address passes another name", request("127.0.0.1:9876", "127.0.0.1:5000", "", "evil.example", false), "host:desk.example", true},
		{"a proxy on this computer that rewrites Host to its upstream address and passes nothing (nginx's default)", request("127.0.0.1:9876", "127.0.0.1:5000", "", "", false), "host:desk.example", true},
		{"a proxy that rewrites Host to localhost", request("localhost:9876", "127.0.0.1:5000", "", "", false), "host:desk.example", true},
		{"X-Forwarded-Host from elsewhere counts for nothing", request("desk.local:9876", "192.168.1.30:5000", "", "evil.example", false), "host:evil.example", false},
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

// Security review 11-23, round 2 F1: the reviewer's four requests, each from the
// peer it comes from, and the tunnel setups our docs describe, with the headers
// measured at a header echo behind them (RESULT.md, 2026-11-23 round 2). The
// binding holds where a tunnel keeps the public Host; a plain listener or a
// proxy that rewrites Host to an address carries none (docs/security-and-privacy.md).
func TestRequestBindingOnPlainHTTP(t *testing.T) {
	srv := New(nil, nil, nil, nil, nil, nil, nil)
	request := func(host, remote, forwarded string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Host, r.RemoteAddr, r.TLS = host, remote, nil
		if forwarded != "" {
			r.Header.Set("X-Forwarded-Host", forwarded)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want bool
	}{
		// The reviewer's table (REVIEW-2.md F1). Row 1 was accepted at 9055ad4b8.
		{"Host desk.example.com from loopback, X-Forwarded-Host evil.example", request("desk.example.com", "127.0.0.1:5000", "evil.example"), false},
		{"the same without X-Forwarded-Host", request("desk.example.com", "127.0.0.1:5000", ""), false},
		// Rows 3 and 4 stay accepted, a stated limit: whoever reaches a plain listener sets the Host (a byte
		// forwarder passes the app's own, which names it), so refusing them would stop no relay.
		{"Host the LAN IP (--lan), from the LAN", request("192.168.1.20:9876", "192.168.1.30:5000", ""), true},
		{"Host localhost", request("localhost:9876", "127.0.0.1:5000", ""), true},
		// Cloudflare Tunnel (cloudflared to http://127.0.0.1:9876) keeps the public Host and passes a client's
		// X-Forwarded-Host as it came; tailscale serve keeps the Host and sets X-Forwarded-Host to it.
		{"Cloudflare Tunnel, a forged X-Forwarded-Host", request("desk.example.com", "127.0.0.1:5000", "evil.example"), false},
		{"tailscale serve", request("desk.tail1234.ts.net", "127.0.0.1:5000", "desk.tail1234.ts.net"), false},
		{"a proxy that rewrites Host to a name of its own and sets X-Forwarded-Host (outside the documented setups)", request("mewla:9876", "172.17.0.2:5000", "evil.example"), false},
	}
	for _, c := range cases {
		if got := srv.requestBinding(c.r)("host:evil.example"); got != c.want {
			t.Errorf("%s: host:evil.example taken = %v, want %v", c.name, got, c.want)
		}
	}
	// What the setups that keep the Host are bound to still passes.
	if !srv.requestBinding(request("desk.example.com", "127.0.0.1:5000", "evil.example"))("host:desk.example.com") {
		t.Error("Cloudflare Tunnel refused the binding to its own public name")
	}
	if !srv.requestBinding(request("desk.tail1234.ts.net", "127.0.0.1:5000", "desk.tail1234.ts.net"))("host:desk.tail1234.ts.net") {
		t.Error("tailscale serve refused the binding to its own name")
	}
}

// Over TLS this daemon terminates (Mewla Relay, the hosted address), a connect
// made for another host is refused on the real SNI, whatever Host it carries.
func TestAConnectMadeForAnotherHostIsRefusedOnTheTLSServerName(t *testing.T) {
	manager, privateKey := enrolledTestDevice(t)
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	defer srv.shutdownAuthenticatedClients()
	host := httptest.NewTLSServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(host.Close)
	url := "wss" + strings.TrimPrefix(host.URL, "https")
	dial := func(binding, hostHeader string) (int, error) {
		header, _ := boundConnectHeader(privateKey, manager.DaemonID(), "device-direct", binding)
		dialer := websocket.Dialer{TLSClientConfig: &tls.Config{ServerName: "quiet-otter.m.mewla.ai", InsecureSkipVerify: true}}
		request := http.Header{"Authorization": {header}}
		if hostHeader != "" {
			request.Set("Host", hostHeader)
		}
		conn, response, err := dialer.Dial(url, request)
		if conn != nil {
			conn.Close()
		}
		if response == nil {
			return 0, err
		}
		return response.StatusCode, err
	}
	if status, err := dial("host:quiet-otter.m.mewla.ai", ""); status != http.StatusSwitchingProtocols {
		t.Fatalf("its own name: %d %v", status, err)
	}
	if status, _ := dial("host:evil.example", ""); status != http.StatusUnauthorized {
		t.Fatalf("made for evil.example: %d", status)
	}
	if status, _ := dial("host:evil.example", "evil.example"); status != http.StatusUnauthorized {
		t.Fatalf("made for evil.example, with its Host: %d", status)
	}
}

// /health says this daemon proves itself, so an app that holds it to proofs can
// name an older Mewla that does not.
func TestHealthSaysTheDaemonProvesItself(t *testing.T) {
	manager, _ := enrolledTestDevice(t)
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	srv.handleHealth(recorder, httptest.NewRequest(http.MethodGet, "/health?challenge=00112233445566778899aabbccddeeff", nil))
	if !strings.Contains(recorder.Body.String(), `"connect_proof":true`) {
		t.Fatalf("health = %s", recorder.Body.String())
	}
}
