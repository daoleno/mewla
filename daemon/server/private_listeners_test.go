package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
)

func testTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

// privateTestServer runs the private listeners on loopback aliases, with the
// address source and the port under the test's control.
func privateTestServer(t *testing.T, detect func() []net.IP) (*Server, int) {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetTLSConfig(testTLSConfig(t))
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	srv.EnablePrivateListeners(port, detect)
	srv.private.allow = func(ip net.IP) bool { return ip.To4() != nil && ip.IsLoopback() }
	return srv, port
}

func startPrivate(t *testing.T, srv *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var done sync.WaitGroup
	done.Add(1)
	go func() { defer done.Done(); srv.runPrivateListeners(ctx) }()
	t.Cleanup(func() { cancel(); done.Wait() })
}

func tlsClient() *http.Client {
	return &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
}

func waitEndpoints(t *testing.T, srv *Server, want int) []DirectEndpoint {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := srv.PrivateEndpoints()
		if len(got) == want || time.Now().After(deadline) {
			if len(got) != want {
				t.Fatalf("endpoints = %v, want %d", got, want)
			}
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPrivateListenerServesTLSAndRefusesPlaintext(t *testing.T) {
	srv, port := privateTestServer(t, func() []net.IP { return []net.IP{net.ParseIP("127.0.0.2")} })
	startPrivate(t, srv)
	endpoints := waitEndpoints(t, srv, 1)
	if endpoints[0].Host != "127.0.0.2" || endpoints[0].Port != port || endpoints[0].Kind != "lan" {
		t.Fatalf("endpoints = %v", endpoints)
	}
	base := net.JoinHostPort("127.0.0.2", strconv.Itoa(port))

	response, err := tlsClient().Get("https://" + base + "/health?challenge=00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if response.StatusCode != 200 || payload["assertion_purpose"] != auth.HealthChallengePurpose {
		t.Fatalf("TLS health: %d %s", response.StatusCode, body)
	}

	// Plain HTTP on the same port is refused and reveals nothing.
	plain, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://" + base + "/health")
	if err == nil {
		defer plain.Body.Close()
		leaked, _ := io.ReadAll(plain.Body)
		if plain.StatusCode == 200 || strings.Contains(string(leaked), "daemon_id") {
			t.Fatalf("plaintext HTTP was served: %d %s", plain.StatusCode, leaked)
		}
		if plain.StatusCode != http.StatusBadRequest {
			t.Fatalf("plaintext status = %d, want 400", plain.StatusCode)
		}
	}
}

func TestPrivateListenerOnlyServesDeviceSignedAndProofRoutes(t *testing.T) {
	srv, port := privateTestServer(t, func() []net.IP { return []net.IP{net.ParseIP("127.0.0.2")} })
	startPrivate(t, srv)
	waitEndpoints(t, srv, 1)
	base := "https://" + net.JoinHostPort("127.0.0.2", strconv.Itoa(port))
	client := tlsClient()

	for _, path := range []string{"/pair", "/p/abc", "/enrollment/request", "/enrollment/pending", "/enrollment/decision", "/plugins/oauth/callback", "/", "/index.html", "/ws/x", "/healthz"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s on the private listener: %d, want 404", path, response.StatusCode)
		}
	}
	// A route that is served still asks for the device signature.
	for _, path := range []string{"/ws", "/auth-check", "/devices", "/upload", "/screen"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s without a device signature: %d", path, response.StatusCode)
		}
	}
}

func TestPrivateListenersFollowTheAddresses(t *testing.T) {
	var mu sync.Mutex
	current := []net.IP{net.ParseIP("127.0.0.2")}
	srv, port := privateTestServer(t, func() []net.IP {
		mu.Lock()
		defer mu.Unlock()
		return append([]net.IP(nil), current...)
	})
	startPrivate(t, srv)
	waitEndpoints(t, srv, 1)

	set := func(ips ...string) {
		mu.Lock()
		current = nil
		for _, ip := range ips {
			current = append(current, net.ParseIP(ip))
		}
		mu.Unlock()
		srv.syncPrivateListeners()
	}
	reachable := func(host string) bool {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	}

	set("127.0.0.2", "127.0.0.3")
	endpoints := waitEndpoints(t, srv, 2)
	if endpoints[0].Host != "127.0.0.2" || endpoints[1].Host != "127.0.0.3" {
		t.Fatalf("endpoints = %v", endpoints)
	}
	if !reachable("127.0.0.3") {
		t.Fatal("a new address must start listening")
	}
	set("127.0.0.3")
	waitEndpoints(t, srv, 1)
	if reachable("127.0.0.2") {
		t.Fatal("an address that went away must stop listening")
	}
	set()
	waitEndpoints(t, srv, 0)
	if reachable("127.0.0.3") {
		t.Fatal("no addresses, no listeners")
	}
}

func TestPrivateListenersNeverOpenOnLoopbackOrPublicAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "0.0.0.0", "169.254.1.1", "8.8.8.8", "100.128.0.1", "172.32.0.1", "::1", "fd00::1"} {
		if privateAddress(net.ParseIP(ip)) {
			t.Fatalf("%s must not get a private listener", ip)
		}
	}
	for ip, kind := range map[string]string{"192.168.1.5": "lan", "10.1.2.3": "lan", "172.16.0.9": "lan", "100.64.0.1": "tailscale", "100.127.9.9": "tailscale"} {
		if !privateAddress(net.ParseIP(ip)) || privateKind(net.ParseIP(ip)) != kind {
			t.Fatalf("%s should be a %s private address", ip, kind)
		}
	}
}

func TestAnAddressAlreadyServedIsLeftAlone(t *testing.T) {
	srv, port := privateTestServer(t, func() []net.IP { return []net.IP{net.ParseIP("127.0.0.2")} })
	taken, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.2", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	startPrivate(t, srv)
	time.Sleep(100 * time.Millisecond)
	if got := srv.PrivateEndpoints(); len(got) != 0 {
		t.Fatalf("an address someone else listens on must not be announced: %v", got)
	}
}
