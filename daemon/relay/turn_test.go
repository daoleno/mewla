package relay

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

var testTURNSecret = bytes.Repeat([]byte{7}, 32)

// anyPort has StartTURN listen where the system picks; srv.Addrs says where.
const anyPort = "127.0.0.1:0"

// hostSocketInRange listens on the first port of the test relays' range
// (61000-61200) that no allocation holds: the relay host's own socket where
// its allocations could be.
func hostSocketInRange(t *testing.T, allocations ...net.PacketConn) net.PacketConn {
	t.Helper()
	held := map[int]bool{}
	for _, allocation := range allocations {
		held[allocation.LocalAddr().(*net.UDPAddr).Port] = true
	}
	for port := 61000; port <= 61200; port++ {
		if held[port] {
			continue
		}
		if c, err := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			return c
		}
	}
	t.Fatal("no free port in 61000-61200")
	return nil
}

func credential(t *testing.T, route string) (string, string) {
	t.Helper()
	issuer, err := NewTURNIssuer(testTURNSecret, []string{"turn:x"}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, user, pass, _, err := issuer.Credentials(route)
	if err != nil {
		t.Fatal(err)
	}
	return user, pass
}

// turnClient is a TURN client over UDP (conn nil) or a stream connection.
func turnClient(t *testing.T, server string, conn net.Conn, user, pass string) *turn.Client {
	t.Helper()
	logs := logging.NewDefaultLoggerFactory()
	logs.DefaultLogLevel = logging.LogLevelError
	cfg := &turn.ClientConfig{STUNServerAddr: server, TURNServerAddr: server, Username: user, Password: pass, Realm: "mewla", LoggerFactory: logs}
	if conn != nil {
		cfg.Conn = turn.NewSTUNConn(conn)
	} else {
		pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		cfg.Conn = pc
	}
	c, err := turn.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// What one credential can reach and hold, as the review's probe asked, must
// fail closed: no TCP allocation, no peer outside public unicast space (the
// relay host's networks, a tailnet, metadata), the relay's own address only at
// another allocation, a few allocations per credential and per computer, and
// TCP connections that send nothing are closed.
func TestTURNOneCredentialReachesLittle(t *testing.T) {
	srv, err := StartTURN(TURNConfig{
		PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: anyPort, TCPAddr: anyPort, Secret: testTURNSecret,
		MinPort: 61000, MaxPort: 61200,
		Limits: TURNLimits{AllocationsPerCredential: 2, AllocationsPerRoute: 3, ConnsPerIP: 4, Idle: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	udpAddr, tcpAddr, _ := srv.Addrs()
	const route = "0123456789abcdef0123456789abcdef"
	user, pass := credential(t, route)

	// No TCP allocation (RFC 6062): no TCP proxy. (Refused after its quota
	// was taken, it holds that for a second: a credential of its own here.)
	stream, err := net.Dial("tcp4", tcpAddr)
	if err != nil {
		t.Fatal(err)
	}
	tcpUser, tcpPass := credential(t, route)
	if _, err := turnClient(t, tcpAddr, stream, tcpUser, tcpPass).AllocateTCP(); err == nil {
		t.Fatal("a TCP allocation was granted")
	}

	// A UDP allocation: it reaches another allocation on this relay, and
	// nothing else of the relay host.
	a := turnClient(t, udpAddr, nil, user, pass)
	relayedA, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	b := turnClient(t, udpAddr, nil, user, pass)
	relayedB, err := b.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	_ = relayedA.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	// A permission at A for B's address lets B's packets in.
	if _, err := relayedA.WriteTo([]byte("hi"), relayedB.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := relayedB.WriteTo([]byte("hello"), relayedA.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if n, _, err := relayedA.ReadFrom(buf); err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("allocation to allocation: %q %v", buf[:n], err)
	}

	// A host socket inside the relayed port range that is no allocation (a
	// Mac's ephemeral ports are 49152-65535, and so is the default range).
	hostService := hostSocketInRange(t, relayedA, relayedB)
	defer hostService.Close()
	_, _ = relayedB.WriteTo([]byte("to-a-host-port"), hostService.LocalAddr())
	_ = hostService.SetReadDeadline(time.Now().Add(time.Second))
	if n, _, err := hostService.ReadFrom(buf); err == nil {
		t.Fatalf("the relay host's own port %v got %q", hostService.LocalAddr(), buf[:n])
	}
	for _, peer := range []string{"100.100.100.200", "100.64.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.169.254", "198.18.0.1", "192.0.0.1", "0.0.0.1", "240.0.0.1"} {
		if _, err := relayedB.WriteTo([]byte("x"), &net.UDPAddr{IP: net.ParseIP(peer), Port: 9}); err == nil {
			t.Errorf("a permission for %s was granted", peer)
		}
	}

	// Quotas: two per credential, three per computer.
	if _, err := turnClient(t, udpAddr, nil, user, pass).Allocate(); err == nil {
		t.Fatal("a third allocation for one credential")
	}
	user2, pass2 := credential(t, route)
	if _, err := turnClient(t, udpAddr, nil, user2, pass2).Allocate(); err != nil {
		t.Fatalf("another credential's first allocation: %v", err)
	}
	if _, err := turnClient(t, udpAddr, nil, user2, pass2).Allocate(); err == nil {
		t.Fatal("a fourth allocation for one computer")
	}
	if total, ofRoute := srv.Allocations(route); total != 3 || ofRoute != 3 {
		t.Fatalf("allocations %d, of the route %d", total, ofRoute)
	}

	// TCP connections that send nothing are closed after Idle; past four from
	// one address, a connection is closed at once.
	var idle []net.Conn
	for range 6 {
		c, err := net.Dial("tcp4", tcpAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		idle = append(idle, c)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, c := range idle {
		_ = c.SetReadDeadline(deadline)
		_, err := c.Read(make([]byte, 1))
		var ne net.Error
		if err == nil || (errors.As(err, &ne) && ne.Timeout()) {
			t.Fatalf("an idle connection stayed open: %v", err)
		}
	}
}

// Allocations asked at once, each over its own TCP connection (the server
// serves each connection on its own), stay within the credential's cap: the
// quota is taken when it is checked, not only once the allocation exists.
func TestTURNQuotaHoldsUnderConcurrentAllocations(t *testing.T) {
	srv, err := StartTURN(TURNConfig{
		PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: anyPort, TCPAddr: anyPort, Secret: testTURNSecret,
		MinPort: 61000, MaxPort: 61200,
		Limits: TURNLimits{AllocationsPerCredential: 2, ConnsPerIP: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, tcpAddr, _ := srv.Addrs()
	const route = "0123456789abcdef0123456789abcdef"
	user, pass := credential(t, route)
	const tries = 12
	clients := make([]*turn.Client, tries)
	for i := range clients {
		stream, err := net.Dial("tcp4", tcpAddr)
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = turnClient(t, tcpAddr, stream, user, pass)
	}
	var wg sync.WaitGroup
	var granted atomic.Int32
	start := make(chan struct{})
	for _, c := range clients {
		wg.Go(func() {
			<-start
			if _, err := c.Allocate(); err == nil {
				granted.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if n := granted.Load(); n > 2 {
		t.Fatalf("%d allocations granted to one credential, cap 2", n)
	}
	if total, _ := srv.Allocations(route); total > 2 {
		t.Fatalf("%d allocations counted, cap 2", total)
	}
}

// A quota taken for an allocation the server then fails to make (a TCP one)
// comes back at that client's next request, or after a second.
func TestTURNQuotaOfAFailedAllocationComesBack(t *testing.T) {
	srv, err := StartTURN(TURNConfig{
		PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: anyPort, TCPAddr: anyPort, Secret: testTURNSecret,
		MinPort: 61000, MaxPort: 61200,
		Limits: TURNLimits{AllocationsPerCredential: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	udpAddr, tcpAddr, _ := srv.Addrs()
	const route = "0123456789abcdef0123456789abcdef"
	refusedTCP := func(user, pass string) *turn.Client {
		stream, err := net.Dial("tcp4", tcpAddr)
		if err != nil {
			t.Fatal(err)
		}
		c := turnClient(t, tcpAddr, stream, user, pass)
		if _, err := c.AllocateTCP(); err == nil {
			t.Fatal("a TCP allocation was granted")
		}
		return c
	}

	// The same client asks again: its earlier request failed.
	user, pass := credential(t, route)
	if _, err := refusedTCP(user, pass).Allocate(); err != nil {
		t.Fatalf("the client's next allocation: %v", err)
	}
	if total, _ := srv.Allocations(route); total != 1 {
		t.Fatalf("%d allocations counted, want 1", total)
	}

	// Another client: the quota is held a second, then given back.
	user2, pass2 := credential(t, route)
	refusedTCP(user2, pass2)
	if _, err := turnClient(t, udpAddr, nil, user2, pass2).Allocate(); err == nil {
		t.Fatal("the failed allocation's quota was free at once")
	}
	time.Sleep(reserveFor + 100*time.Millisecond)
	if _, err := turnClient(t, udpAddr, nil, user2, pass2).Allocate(); err != nil {
		t.Fatalf("after a second: %v", err)
	}
	if total, _ := srv.Allocations(route); total != 2 {
		t.Fatalf("%d allocations counted, want 2", total)
	}
}

// Past ConnsPerIP connections from one address, a TCP connection is closed at
// once while the ones before it stay open (Idle is long here, so only the cap
// can close them), and a closed one gives its slot back.
func TestTURNCapsConnectionsPerAddress(t *testing.T) {
	srv, err := StartTURN(TURNConfig{
		PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: anyPort, TCPAddr: anyPort, Secret: testTURNSecret,
		Limits: TURNLimits{ConnsPerIP: 2, Idle: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, tcpAddr, _ := srv.Addrs()
	dial := func() net.Conn {
		c, err := net.Dial("tcp4", tcpAddr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	// open reports whether the server still holds c after a moment.
	open := func(c net.Conn) bool {
		_ = c.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
		_, err := c.Read(make([]byte, 1))
		var ne net.Error
		return errors.As(err, &ne) && ne.Timeout()
	}
	first, second := dial(), dial()
	time.Sleep(200 * time.Millisecond) // the server's first reads take the two slots
	third := dial()
	if open(third) {
		t.Fatal("a third connection from one address stayed open")
	}
	if !open(first) || !open(second) {
		t.Fatal("a connection under the cap was closed")
	}
	_ = first.Close()
	time.Sleep(200 * time.Millisecond)
	if !open(dial()) {
		t.Fatal("a closed connection's slot was not given back")
	}
}

// Credentials signed with the shared secret outlive the TURN process: a new
// one with the same secret file accepts them.
func TestTURNCredentialsSurviveARestart(t *testing.T) {
	user, pass := credential(t, "0123456789abcdef0123456789abcdef")
	for range 2 {
		srv, err := StartTURN(TURNConfig{PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: anyPort, Secret: testTURNSecret})
		if err != nil {
			t.Fatal(err)
		}
		udpAddr, _, _ := srv.Addrs()
		if _, err := turnClient(t, udpAddr, nil, user, pass).Allocate(); err != nil {
			t.Fatal(err)
		}
		_ = srv.Close()
	}
	other := bytes.Repeat([]byte{8}, 32)
	srv, err := StartTURN(TURNConfig{PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: anyPort, Secret: other})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	udpAddr, _, _ := srv.Addrs()
	if _, err := turnClient(t, udpAddr, nil, user, pass).Allocate(); err == nil {
		t.Fatal("a credential under another secret allocated")
	}
}

// TURN over TLS behind the relay's 443 SNI route, as the hosted relay runs it
// (-sni-route-proxy-protocol): the connection starts with a PROXY header.
func TestTURNOverTLSBehindAProxyHeader(t *testing.T) {
	srv, err := StartTURN(TURNConfig{
		PublicIP: net.IPv4(127, 0, 0, 1), TLSAddr: anyPort, ProxyProtocol: true, Secret: testTURNSecret,
		TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{selfSignedCertificate(t, "turn.relay.test")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, _, tlsAddr := srv.Addrs()
	raw, err := net.Dial("tcp4", tlsAddr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(raw, "PROXY TCP4 203.0.113.9 127.0.0.1 40000 443\r\n"); err != nil {
		t.Fatal(err)
	}
	conn := tls.Client(raw, &tls.Config{ServerName: "turn.relay.test", InsecureSkipVerify: true}) //nolint:gosec // a test certificate
	if err := conn.Handshake(); err != nil {
		t.Fatal(err)
	}
	user, pass := credential(t, "0123456789abcdef0123456789abcdef")
	relayed, err := turnClient(t, tlsAddr, conn, user, pass).Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if port := relayed.LocalAddr().(*net.UDPAddr).Port; port < 49152 {
		t.Fatalf("relayed port %d", port)
	}
}

func TestPublicPeer(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "167.114.116.27": true,
		"127.0.0.1": false, "::ffff:127.0.0.1": false, "10.1.2.3": false, "100.64.0.1": false, "100.127.255.254": false,
		"169.254.169.254": false, "172.31.0.1": false, "192.168.0.1": false, "192.0.0.8": false, "198.19.0.1": false,
		"0.1.2.3": false, "240.0.0.1": false, "255.255.255.255": false, "224.0.0.1": false,
		"::1": false, "fd00::1": false, "fe80::1": false, "64:ff9b::a00:1": false, "2002:a00:1::1": false,
		"2001:db8::1": false, "2001::1": false, "ff02::1": false,
	} {
		if got := publicPeer(net.ParseIP(ip)); got != want {
			t.Errorf("publicPeer(%s) = %v", ip, got)
		}
	}
}

func TestBucketHoldsTheRate(t *testing.T) {
	b := newBucket(800) // 100 kB/s, 100 kB at once
	sent := 0
	for b.take(1200) {
		sent += 1200
	}
	if sent > 100_000 || sent < 98_000 {
		t.Fatalf("burst %d bytes", sent)
	}
	time.Sleep(100 * time.Millisecond)
	more := 0
	for b.take(1200) {
		more += 1200
	}
	if more < 6_000 || more > 14_000 {
		t.Fatalf("refill after 100 ms: %d bytes", more)
	}
}
