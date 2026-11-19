package relay

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/daoleno/mewla/daemon/proxyproto"
)

// TURN carries Screen's video when a phone and its computer have no direct
// path (mobile data, a strict NAT). UDP first, and TCP and TLS to the TURN
// server for networks that block UDP (TLS can share port 443 through a static
// SNI route, -sni-route). The media in it is DTLS-SRTP between the phone and
// the computer, so the relay forwards what it cannot read.
//
// Credentials are TURN REST style: "<expiry>:<route>:<nonce>" with an HMAC of
// it under a secret the relay and its TURN server share (a file, so they
// survive a restart and may run as two processes). The relay issues them only
// to a computer that proves it is a registered route (the signed
// turn_request); the computer asks for them when an allowed device opens a
// view, and hands them to that viewer.
//
// What one credential can do is narrow: UDP allocations only (no TCP
// allocations, so no TCP proxy), peers that are public unicast addresses or
// another allocation on this relay (never the relay host's own ports, its
// loopback, private networks, shared address space or cloud metadata), a few
// allocations per credential and per computer, a bit rate per allocation and
// in all. TCP and TLS connections to the server are capped in all and per
// address and closed when idle.

// TURNLimits bound what TURN may use. Zero is the default.
type TURNLimits struct {
	// AllocationsPerCredential (default 4), AllocationsPerRoute (default
	// 32: a computer and its viewers, five views at six) and Allocations in
	// all (default 512).
	AllocationsPerCredential int
	AllocationsPerRoute      int
	Allocations              int
	// Kbps is the most one allocation relays each way (default 8000);
	// TotalKbps the most all of them do together, each way (default 400000).
	// Past it, packets are dropped; video's congestion control backs off.
	Kbps      int
	TotalKbps int
	// Conns caps TCP and TLS connections to the server in all (default 512),
	// ConnsPerIP from one address (default 16).
	Conns      int
	ConnsPerIP int
	// Idle closes a TCP or TLS connection that sent nothing for this long
	// (default 30 s; ICE consent checks run every few seconds).
	Idle time.Duration
}

func (l TURNLimits) withDefaults() TURNLimits {
	or := func(v, d int) int {
		if v <= 0 {
			return d
		}
		return v
	}
	l.AllocationsPerCredential = or(l.AllocationsPerCredential, 4)
	l.AllocationsPerRoute = or(l.AllocationsPerRoute, 32)
	l.Allocations = or(l.Allocations, 512)
	l.Kbps = or(l.Kbps, 8000)
	l.TotalKbps = or(l.TotalKbps, 400000)
	l.Conns = or(l.Conns, 512)
	l.ConnsPerIP = or(l.ConnsPerIP, 16)
	if l.Idle <= 0 {
		l.Idle = 30 * time.Second
	}
	return l
}

// ReadTURNSecret reads the shared secret: at least 32 bytes, as hex or raw.
func ReadTURNSecret(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("TURN secret: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	if decoded, err := hex.DecodeString(text); err == nil {
		raw = decoded
	}
	if len(raw) < 32 {
		return nil, errors.New("TURN secret: need at least 32 bytes")
	}
	return raw, nil
}

// TURNIssuer issues credentials for the TURN server that shares its secret.
type TURNIssuer struct {
	secret  string
	urls    []string
	ttl     time.Duration
	maxKbps int
}

// NewTURNIssuer issues credentials lasting ttl (default 12 h) for urls, and
// tells computers the per-allocation rate (maxKbps, 0: none told).
func NewTURNIssuer(secret []byte, urls []string, ttl time.Duration, maxKbps int) (*TURNIssuer, error) {
	if len(secret) < 32 {
		return nil, errors.New("TURN needs a secret of at least 32 bytes")
	}
	if len(urls) == 0 {
		return nil, errors.New("TURN needs at least one URL to tell clients")
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &TURNIssuer{secret: hex.EncodeToString(secret), urls: append([]string(nil), urls...), ttl: ttl, maxKbps: max(maxKbps, 0)}, nil
}

// Credentials issues a new credential for one route, its own username.
func (i *TURNIssuer) Credentials(route string) (urls []string, username, password string, expires time.Time, err error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", "", time.Time{}, err
	}
	username, password, err = turn.GenerateLongTermTURNRESTCredentials(i.secret, route+":"+hex.EncodeToString(nonce), i.ttl)
	if err != nil {
		return nil, "", "", time.Time{}, err
	}
	return append([]string(nil), i.urls...), username, password, time.Now().Add(i.ttl), nil
}

// MaxKbps is the most one allocation relays each way (0: no cap told).
func (i *TURNIssuer) MaxKbps() int {
	if i == nil {
		return 0
	}
	return i.maxKbps
}

// TURNConfig configures the TURN server. Zero addresses are not served.
type TURNConfig struct {
	// PublicIP is the address allocations are reached at.
	PublicIP net.IP
	// UDPAddr, TCPAddr and TLSAddr are listen addresses (":3478",
	// "127.0.0.1:5349").
	UDPAddr string
	TCPAddr string
	TLSAddr string
	// TLS is the certificate for TLSAddr.
	TLS *tls.Config
	// ProxyProtocol: connections to TLSAddr start with a PROXY v1 header
	// (the relay's static SNI routes send one with -sni-route-proxy-protocol).
	ProxyProtocol bool
	// Secret is shared with the TURNIssuer.
	Secret []byte
	// MinPort and MaxPort bound the relayed addresses' UDP ports (default
	// 49152-65535).
	MinPort, MaxPort uint16
	Limits           TURNLimits
	// Logf receives operational events.
	Logf func(format string, args ...any)
}

// TURNServer is a running TURN server.
type TURNServer struct {
	server *turn.Server
	limits TURNLimits

	mu      sync.Mutex
	total   int
	byRoute map[string]int
	byCred  map[string]int
	// pending are quotas taken by admit whose allocation is not made yet: the
	// server checks the quota, then allocates, on each connection's own
	// goroutine, so a quota only counted once the allocation exists lets
	// requests at once all pass.
	pending []reservation
}

// reservation is one quota admit took, for username's request from client,
// at at.
type reservation struct {
	username, client string
	at               time.Time
}

// reserveFor is how long a quota admit took waits for its allocation: the
// server says nothing of one it failed to make (a TCP allocation, no port
// left), so its quota comes back then, or at the client's next request.
const reserveFor = time.Second

// StartTURN listens and serves TURN until Close.
func StartTURN(config TURNConfig) (*TURNServer, error) {
	public := config.PublicIP.To4()
	if public == nil {
		return nil, errors.New("TURN needs the relay's public IPv4 address")
	}
	if len(config.Secret) < 32 {
		return nil, errors.New("TURN needs a secret of at least 32 bytes")
	}
	limits := config.Limits.withDefaults()
	minPort, maxPort := config.MinPort, config.MaxPort
	if minPort == 0 {
		minPort = 49152
	}
	if maxPort == 0 {
		maxPort = 65535
	}
	if minPort > maxPort {
		return nil, fmt.Errorf("TURN port range %d-%d is empty", minPort, maxPort)
	}
	t := &TURNServer{limits: limits, byRoute: map[string]int{}, byCred: map[string]int{}}
	total := [2]*bucket{newBucket(limits.TotalKbps), newBucket(limits.TotalKbps)}
	allocated := &allocatedPorts{ports: map[int]int{}}
	relayAddress := func() turn.RelayAddressGenerator {
		return &udpOnlyRelay{
			RelayAddressGeneratorPortRange: &turn.RelayAddressGeneratorPortRange{RelayAddress: public, Address: "0.0.0.0", MinPort: minPort, MaxPort: maxPort},
			own:                            public, allocated: allocated, kbps: limits.Kbps, total: total,
		}
	}
	permit := func(_ net.Addr, peer net.IP) bool { return peer.Equal(public) || publicPeer(peer) }
	conns := &connLimiter{total: limits.Conns, perIP: limits.ConnsPerIP, byIP: map[string]int{}}

	var packetConns []turn.PacketConnConfig
	var listeners []turn.ListenerConfig
	closeAll := func() {
		for _, p := range packetConns {
			_ = p.PacketConn.Close()
		}
		for _, l := range listeners {
			_ = l.Listener.Close()
		}
	}
	if config.UDPAddr != "" {
		conn, err := net.ListenPacket("udp4", config.UDPAddr)
		if err != nil {
			return nil, fmt.Errorf("TURN UDP: %w", err)
		}
		packetConns = append(packetConns, turn.PacketConnConfig{PacketConn: conn, RelayAddressGenerator: relayAddress(), PermissionHandler: permit})
	}
	if config.TCPAddr != "" {
		l, err := net.Listen("tcp4", config.TCPAddr)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("TURN TCP: %w", err)
		}
		listeners = append(listeners, turn.ListenerConfig{Listener: &guardedListener{Listener: l, limit: conns, idle: limits.Idle}, RelayAddressGenerator: relayAddress(), PermissionHandler: permit})
	}
	if config.TLSAddr != "" {
		if config.TLS == nil {
			closeAll()
			return nil, errors.New("TURN TLS needs a certificate")
		}
		l, err := net.Listen("tcp4", config.TLSAddr)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("TURN TLS: %w", err)
		}
		if config.ProxyProtocol {
			l = &proxyproto.Listener{Listener: l}
		}
		guarded := &guardedListener{Listener: l, limit: conns, idle: limits.Idle}
		listeners = append(listeners, turn.ListenerConfig{Listener: tls.NewListener(guarded, config.TLS), RelayAddressGenerator: relayAddress(), PermissionHandler: permit})
	}
	if len(packetConns)+len(listeners) == 0 {
		return nil, errors.New("TURN has no address to listen on")
	}
	logs := logging.NewDefaultLoggerFactory()
	logs.DefaultLogLevel = logging.LogLevelWarn
	restAuth := turn.LongTermTURNRESTAuthHandler(hex.EncodeToString(config.Secret), logs.NewLogger("turn"))
	server, err := turn.NewServer(turn.ServerConfig{
		Realm: "mewla",
		// The whole username is the user, so quotas count each credential.
		AuthHandler: func(ra *turn.RequestAttributes) (string, []byte, bool) {
			_, key, ok := restAuth(ra)
			return ra.Username, key, ok
		},
		PacketConnConfigs: packetConns,
		ListenerConfigs:   listeners,
		LoggerFactory:     logs,
		QuotaHandler:      func(username, _ string, client net.Addr) bool { return t.admit(username, client.String()) },
		EventHandler: turn.EventHandler{
			OnAllocationCreated: func(client, _ net.Addr, _, username, _ string, _ net.Addr, _ int) {
				t.created(username, client.String())
			},
			OnAllocationDeleted: func(_, _ net.Addr, _, username, _ string) { t.deleted(username) },
		},
	})
	if err != nil {
		closeAll()
		return nil, err
	}
	t.server = server
	if config.Logf != nil {
		config.Logf("turn: serving udp=%q tcp=%q tls=%q on %v, ports %d-%d", config.UDPAddr, config.TCPAddr, config.TLSAddr, public, minPort, maxPort)
	}
	return t, nil
}

// Allocations is how many allocations there are, in all and of a route.
func (t *TURNServer) Allocations(route string) (total, ofRoute int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total, t.byRoute[route]
}

// turnRoute is the route a username ("<expiry>:<route>:<nonce>") is for.
func turnRoute(username string) string {
	fields := strings.Split(username, ":")
	if len(fields) < 2 {
		return username
	}
	return fields[1]
}

// admit takes a quota for one allocation of username, asked by client, when
// there is one left in all, of its route and of the credential.
func (t *TURNServer) admit(username, client string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	live := t.pending[:0]
	for _, r := range t.pending {
		// One client's requests are served in turn: a reservation of its
		// earlier one is an allocation that failed.
		if now.Sub(r.at) < reserveFor && r.client != client {
			live = append(live, r)
		} else {
			t.add(r.username, -1)
		}
	}
	t.pending = live
	if t.total >= t.limits.Allocations ||
		t.byRoute[turnRoute(username)] >= t.limits.AllocationsPerRoute ||
		t.byCred[username] >= t.limits.AllocationsPerCredential {
		return false
	}
	t.add(username, 1)
	t.pending = append(t.pending, reservation{username: username, client: client, at: now})
	return true
}

// created turns the reservation of client's request into its allocation
// (counted already), or counts one whose reservation lapsed.
func (t *TURNServer) created(username, client string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, r := range t.pending {
		if r.username == username && r.client == client {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			return
		}
	}
	t.add(username, 1)
}

func (t *TURNServer) deleted(username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.add(username, -1)
}

// add counts n allocations of username; t.mu is held.
func (t *TURNServer) add(username string, n int) {
	route := turnRoute(username)
	t.total += n
	t.byRoute[route] += n
	t.byCred[username] += n
	if t.byRoute[route] <= 0 {
		delete(t.byRoute, route)
	}
	if t.byCred[username] <= 0 {
		delete(t.byCred, username)
	}
}

// Close stops the server.
func (t *TURNServer) Close() error {
	if t == nil || t.server == nil {
		return nil
	}
	return t.server.Close()
}

// specialRanges are not public: a TURN peer there would reach something on
// the relay host's side (its networks, a tailnet, cloud metadata) or nothing
// real. Global unicast addresses outside them are allowed.
var specialRanges = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32",
		"2002::/16", "3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// publicPeer reports whether a peer is a public unicast address.
func publicPeer(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	for _, n := range specialRanges {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// udpOnlyRelay allocates UDP relayed addresses only (RFC 6062 TCP
// allocations would make TURN a TCP proxy), each held to kbps each way and to
// the shared total, and sending to the relay's own address only at another
// allocation's port.
type udpOnlyRelay struct {
	*turn.RelayAddressGeneratorPortRange
	own       net.IP
	allocated *allocatedPorts
	kbps      int
	total     [2]*bucket
}

// allocatedPorts holds the ports of the live allocations, shared by every
// listener's generator. The relayed range is no proof by itself: the host's
// own sockets can sit in it (Linux's ephemeral ports reach 60999, a Mac's are
// 49152-65535, as is the default range).
type allocatedPorts struct {
	mu    sync.Mutex
	ports map[int]int
}

func (a *allocatedPorts) add(port int) {
	a.mu.Lock()
	a.ports[port]++
	a.mu.Unlock()
}

func (a *allocatedPorts) remove(port int) {
	a.mu.Lock()
	if a.ports[port]--; a.ports[port] <= 0 {
		delete(a.ports, port)
	}
	a.mu.Unlock()
}

func (a *allocatedPorts) holds(port int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ports[port] > 0
}

var errNoTCPAllocations = errors.New("TCP allocations are not served")

func (g *udpOnlyRelay) AllocatePacketConn(config turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	conn, addr, err := g.RelayAddressGeneratorPortRange.AllocatePacketConn(config)
	if err != nil {
		return nil, nil, err
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	g.allocated.add(port)
	return &policedConn{PacketConn: conn, relay: g, port: port, in: newBucket(g.kbps), out: newBucket(g.kbps)}, addr, nil
}

func (g *udpOnlyRelay) AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, errNoTCPAllocations
}

func (g *udpOnlyRelay) AllocateConn(turn.AllocateConnConfig) (net.Conn, error) {
	return nil, errNoTCPAllocations
}

// reaches reports whether an allocation may exchange packets with addr: the
// relay's own address only at a live allocation's port.
func (g *udpOnlyRelay) reaches(addr net.Addr) bool {
	u, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	if u.IP.Equal(g.own) {
		return g.allocated.holds(u.Port)
	}
	return publicPeer(u.IP)
}

// policedConn is one allocation's relayed socket. It drops packets to or
// from addresses it may not reach, and those past its rate (in: from the
// peer, out: to it) or the shared total.
type policedConn struct {
	net.PacketConn
	relay     *udpOnlyRelay
	port      int
	in, out   *bucket
	closeOnce sync.Once
}

// Close ends the allocation; its port is no longer one the others reach.
func (c *policedConn) Close() error {
	c.closeOnce.Do(func() { c.relay.allocated.remove(c.port) })
	return c.PacketConn.Close()
}

func (c *policedConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil {
			return n, addr, err
		}
		if c.relay.reaches(addr) && c.in.take(n) && c.relay.total[0].take(n) {
			return n, addr, nil
		}
	}
}

func (c *policedConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if !c.relay.reaches(addr) || !c.out.take(len(p)) || !c.relay.total[1].take(len(p)) {
		return len(p), nil
	}
	return c.PacketConn.WriteTo(p, addr)
}

// bucket is a token bucket of bytes: rate a second, and a second's worth at
// once (a key frame comes as one burst: there is no pacer).
type bucket struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	at     time.Time
}

func newBucket(kbps int) *bucket {
	rate := float64(kbps) * 1000 / 8
	return &bucket{rate: rate, tokens: rate, at: time.Now()}
}

func (b *bucket) take(n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens = min(b.rate, b.tokens+now.Sub(b.at).Seconds()*b.rate)
	b.at = now
	if b.tokens < float64(n) {
		return false
	}
	b.tokens -= float64(n)
	return true
}

// connLimiter counts TCP and TLS connections to the TURN server.
type connLimiter struct {
	mu           sync.Mutex
	n, total     int
	perIP        int
	byIP         map[string]int
	refusedTotal int
}

func (l *connLimiter) acquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n >= l.total {
		l.refusedTotal++
		return false
	}
	l.n++
	return true
}

func (l *connLimiter) acquireIP(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byIP[ip] >= l.perIP {
		return false
	}
	l.byIP[ip]++
	return true
}

func (l *connLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n--
	if ip != "" {
		if l.byIP[ip]--; l.byIP[ip] <= 0 {
			delete(l.byIP, ip)
		}
	}
}

// guardedListener caps connections in all (at accept) and per address (at the
// first read, after a PROXY header names the client), and closes one that
// sends nothing for idle.
type guardedListener struct {
	net.Listener
	limit *connLimiter
	idle  time.Duration
}

func (l *guardedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if !l.limit.acquire() {
			_ = conn.Close()
			continue
		}
		g := &guardedConn{Conn: conn, limit: l.limit, idle: l.idle}
		g.timer = time.AfterFunc(l.idle, func() { _ = g.Close() })
		return g, nil
	}
}

type guardedConn struct {
	net.Conn
	limit *connLimiter
	idle  time.Duration
	timer *time.Timer

	first     sync.Once
	firstErr  error
	ip        string
	closeOnce sync.Once
}

var errTooManyFromAddress = errors.New("too many TURN connections from this address")

func (c *guardedConn) Read(p []byte) (int, error) {
	c.first.Do(func() {
		ip := c.Conn.RemoteAddr().String()
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		if !c.limit.acquireIP(ip) {
			c.firstErr = errTooManyFromAddress
			return
		}
		c.ip = ip
	})
	if c.firstErr != nil {
		_ = c.Close()
		return 0, c.firstErr
	}
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.timer.Reset(c.idle)
	}
	return n, err
}

func (c *guardedConn) Close() error {
	err := c.Conn.Close()
	c.closeOnce.Do(func() {
		c.timer.Stop()
		c.first.Do(func() {}) // a connection closed before its first read holds no address slot
		c.limit.release(c.ip)
	})
	return err
}
