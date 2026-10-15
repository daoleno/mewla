package relay

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/proxyproto"
)

const testRelayToken = "abcdef0123456789abcdef0123456789"

type relayFixture struct {
	server        *Server
	clientAddr    string
	connectorAddr string
	serviceKey    ed25519.PrivateKey
}

func startRelayFixture(t *testing.T, configure func(*Config)) *relayFixture {
	t.Helper()
	servicePublic, servicePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig(testRelayToken)
	config.GrantKeys = []ed25519.PublicKey{servicePublic}
	config.AttachTimeout = 3 * time.Second
	if configure != nil {
		configure(&config)
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	connectorListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = server.Serve(ctx, clientListener, connectorListener)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("relay fixture did not stop")
		}
	})
	return &relayFixture{
		server:        server,
		clientAddr:    clientListener.Addr().String(),
		connectorAddr: connectorListener.Addr().String(),
		serviceKey:    servicePrivate,
	}
}

func newDaemonKey(t *testing.T) *auth.Manager {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func (fixture *relayFixture) grant(
	t *testing.T,
	manager *auth.Manager,
	routeID string,
	label string,
	ttl time.Duration,
) string {
	t.Helper()
	now := time.Now()
	grant, err := linkproto.SignRouteGrant(fixture.serviceKey, linkproto.RouteGrant{
		AccountID:       "github:1",
		MachineID:       "machine-1",
		RouteID:         routeID,
		Label:           label,
		DaemonPublicKey: manager.PublicKeyHex(),
		IssuedAtMS:      now.UnixMilli(),
		ExpiresAtMS:     now.Add(ttl).UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

// linkRequest builds a signed connector request. A grant selects hosted
// authentication; otherwise the shared token is attached.
func linkRequest(
	t *testing.T,
	manager *auth.Manager,
	messageType string,
	routeID string,
	grant string,
	edit func(*linkproto.Message),
) linkproto.Message {
	t.Helper()
	nonce, err := linkproto.RandomID(16)
	if err != nil {
		t.Fatal(err)
	}
	message := linkproto.Message{
		Type:            messageType,
		RouteID:         routeID,
		DaemonID:        manager.DaemonID(),
		DaemonPublicKey: manager.PublicKeyHex(),
		TimestampMS:     time.Now().UnixMilli(),
		Nonce:           nonce,
		Grant:           grant,
	}
	if grant == "" {
		message.ConnectorToken = testRelayToken
	}
	if edit != nil {
		edit(&message)
	}
	message.Signature = manager.CreateLinkSignature(
		linkproto.SignaturePayload(message),
	)
	return message
}

func (fixture *relayFixture) exchange(
	t *testing.T,
	message linkproto.Message,
) (net.Conn, linkproto.Message) {
	t.Helper()
	conn, err := net.Dial("tcp", fixture.connectorAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := linkproto.WriteMessage(conn, message); err != nil {
		t.Fatal(err)
	}
	response, err := linkproto.ReadMessage(conn)
	if err != nil {
		t.Fatalf("read %s response: %v", message.Type, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, response
}

func (fixture *relayFixture) register(
	t *testing.T,
	message linkproto.Message,
) net.Conn {
	t.Helper()
	conn, response := fixture.exchange(t, message)
	if response.Type != linkproto.TypeRegistered {
		t.Fatalf("register response=%#v", response)
	}
	return conn
}

func (fixture *relayFixture) requireRejected(
	t *testing.T,
	message linkproto.Message,
	contains string,
) {
	t.Helper()
	_, response := fixture.exchange(t, message)
	if response.Type != linkproto.TypeError ||
		!strings.Contains(response.Message, contains) {
		t.Fatalf("%s response=%#v, want error containing %q", message.Type, response, contains)
	}
}

// dialClient starts a TLS handshake through the relay client port.
func (fixture *relayFixture) dialClient(t *testing.T, serverName string) net.Conn {
	t.Helper()
	raw, err := net.Dial("tcp", fixture.clientAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	client := tls.Client(raw, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		ServerName:         serverName,
		InsecureSkipVerify: true,
	})
	go func() { _ = client.Handshake() }()
	return raw
}

func readControl(t *testing.T, control net.Conn, wantType string) linkproto.Message {
	t.Helper()
	_ = control.SetReadDeadline(time.Now().Add(2 * time.Second))
	message, err := linkproto.ReadMessage(control)
	if err != nil {
		t.Fatalf("read %s: %v", wantType, err)
	}
	if message.Type != wantType {
		t.Fatalf("control message=%#v, want %s", message, wantType)
	}
	return message
}

func requireClosed(t *testing.T, conn net.Conn, within time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(within))
	buffer := make([]byte, 512)
	for {
		if _, err := conn.Read(buffer); err != nil {
			if isTimeout(err) {
				t.Fatal("connection stayed open")
			}
			return
		}
	}
}

func (fixture *relayFixture) attach(
	t *testing.T,
	open linkproto.Message,
) net.Conn {
	t.Helper()
	conn, response := fixture.exchange(t, linkproto.Message{
		Type:         linkproto.TypeAttachStream,
		StreamID:     open.StreamID,
		StreamTicket: open.StreamTicket,
	})
	if response.Type != linkproto.TypeAttached {
		t.Fatalf("attach response=%#v", response)
	}
	return conn
}

func requireClientHello(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatalf("read forwarded ClientHello: %v", err)
	}
	if header[0] != 22 {
		t.Fatalf("forwarded bytes start with %d, want a TLS handshake record", header[0])
	}
}

func TestHostedRelayRequiresGrantBoundToRouteAndKey(t *testing.T) {
	fixture := startRelayFixture(t, func(config *Config) {
		config.ConnectorToken = ""
	})
	daemon := newDaemonKey(t)
	other := newDaemonKey(t)
	routeID := strings.Repeat("1", 32)
	label := "studio-3f9a2c"

	fixture.requireRejected(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID, "", nil),
		"connector authentication failed")
	fixture.requireRejected(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID,
		fixture.grant(t, other, routeID, label, time.Hour), nil),
		"does not match")
	fixture.requireRejected(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID,
		fixture.grant(t, daemon, strings.Repeat("2", 32), label, time.Hour), nil),
		"does not match")
	_, foreignService, _ := ed25519.GenerateKey(rand.Reader)
	foreign := &relayFixture{serviceKey: foreignService}
	fixture.requireRejected(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID,
		foreign.grant(t, daemon, routeID, label, time.Hour), nil),
		"signature is invalid")

	grant := fixture.grant(t, daemon, routeID, label, time.Hour)
	control := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID, grant, nil))

	// Label SNI resolves to the hosted route; route ID SNI still works.
	fixture.dialClient(t, label+".link.test")
	open := readControl(t, control, linkproto.TypeOpenStream)
	if open.ClientAddr != "127.0.0.1" {
		t.Fatalf("open_stream client_addr = %q, want the client's IP", open.ClientAddr)
	}
	requireClientHello(t, fixture.attach(t, open))
	fixture.dialClient(t, routeID+".link.test")
	readControl(t, control, linkproto.TypeOpenStream)
	requireClosed(t, fixture.dialClient(t, "other-3f9a2c.link.test"), 2*time.Second)

	// A browser's parallel connections from one address all route.
	for range 12 {
		fixture.dialClient(t, label+".link.test")
	}
	for range 12 {
		readControl(t, control, linkproto.TypeOpenStream)
	}

	fixture.requireRejected(t, linkRequest(t, daemon, linkproto.TypeAdmissionRequest, routeID, "",
		func(message *linkproto.Message) { message.TTLSeconds = 60 }),
		"connector authentication failed")
}

func TestHostedAdmissionsRequireGrantAndStayOneTime(t *testing.T) {
	fixture := startRelayFixture(t, nil) // accepts token and grants
	daemon := newDaemonKey(t)
	routeID := strings.Repeat("3", 32)
	grant := fixture.grant(t, daemon, routeID, "laptop-0a1b2c", time.Hour)
	control := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID, grant, nil))

	fixture.requireRejected(t, linkRequest(t, daemon, linkproto.TypeAdmissionRequest, routeID, "",
		func(message *linkproto.Message) { message.TTLSeconds = 60 }),
		"requires a route grant")
	_, response := fixture.exchange(t, linkRequest(t, daemon, linkproto.TypeAdmissionRequest, routeID, grant,
		func(message *linkproto.Message) { message.TTLSeconds = 60 }))
	if response.Type != linkproto.TypeAdmissionResponse || !linkproto.IsOpaqueID(response.Alias, 16) {
		t.Fatalf("admission response=%#v", response)
	}
	alias := response.Alias

	fixture.dialClient(t, alias+".link.test")
	open := readControl(t, control, linkproto.TypeOpenStream)
	if open.Alias != alias {
		t.Fatalf("open stream alias=%q want %q", open.Alias, alias)
	}
	consume := func(grant string) linkproto.Message {
		return linkRequest(t, daemon, linkproto.TypeAdmissionConsume, routeID, grant,
			func(message *linkproto.Message) {
				message.Alias = alias
				message.StreamID = open.StreamID
			})
	}
	fixture.requireRejected(t, consume(""), "requires a route grant")
	if _, response := fixture.exchange(t, consume(grant)); response.Type != linkproto.TypeAdmissionConsumed {
		t.Fatalf("consume response=%#v", response)
	}
	fixture.requireRejected(t, consume(grant), "already consumed")
}

func TestForeignKeyRegisterIsRejectedInTokenAndHostedModes(t *testing.T) {
	t.Run("token", func(t *testing.T) {
		fixture := startRelayFixture(t, func(config *Config) {
			config.MaxRouteBindings = 2
		})
		owner := newDaemonKey(t)
		squatter := newDaemonKey(t)
		routeID := strings.Repeat("4", 32)
		control := fixture.register(t, linkRequest(t, owner, linkproto.TypeRegister, routeID, "", nil))
		fixture.requireRejected(t, linkRequest(t, squatter, linkproto.TypeRegister, routeID, "", nil),
			"another daemon")

		// The binding survives the owner disconnecting.
		_ = control.Close()
		waitForRelayCondition(t, 2*time.Second, func() bool {
			return fixture.server.Snapshot().ActiveRoutes == 0
		})
		fixture.requireRejected(t, linkRequest(t, squatter, linkproto.TypeRegister, routeID, "", nil),
			"bound to another daemon")

		// Bindings are bounded and count toward their own capacity.
		fixture.register(t, linkRequest(t, squatter, linkproto.TypeRegister, strings.Repeat("5", 32), "", nil))
		fixture.requireRejected(t, linkRequest(t, squatter, linkproto.TypeRegister, strings.Repeat("6", 32), "", nil),
			"binding capacity")
		if got := fixture.server.capacityRejects["route_bindings"].Load(); got != 1 {
			t.Fatalf("route_bindings rejections=%d", got)
		}

		// An expired binding frees the route for a new key.
		fixture.server.mu.Lock()
		binding := fixture.server.bindings[routeID]
		binding.expiresAt = time.Now().Add(-time.Second)
		fixture.server.bindings[routeID] = binding
		fixture.server.mu.Unlock()
		fixture.register(t, linkRequest(t, squatter, linkproto.TypeRegister, routeID, "", nil))

		// A full binding table never blocks a hosted route.
		hostedRoute := strings.Repeat("d", 32)
		fixture.register(t, linkRequest(t, owner, linkproto.TypeRegister, hostedRoute,
			fixture.grant(t, owner, hostedRoute, "full-d0d0d0", time.Hour), nil))
	})

	t.Run("hosted", func(t *testing.T) {
		fixture := startRelayFixture(t, nil)
		owner := newDaemonKey(t)
		other := newDaemonKey(t)
		routeID := strings.Repeat("7", 32)
		label := "desk-a1b2c3"
		fixture.register(t, linkRequest(t, owner, linkproto.TypeRegister, routeID,
			fixture.grant(t, owner, routeID, label, time.Hour), nil))
		fixture.requireRejected(t, linkRequest(t, other, linkproto.TypeRegister, routeID,
			fixture.grant(t, other, routeID, label, time.Hour), nil),
			"another daemon")
		fixture.requireRejected(t, linkRequest(t, other, linkproto.TypeRegister, routeID, "", nil),
			"another daemon")
	})

	t.Run("grant takes over token route", func(t *testing.T) {
		fixture := startRelayFixture(t, nil)
		squatter := newDaemonKey(t)
		owner := newDaemonKey(t)
		routeID := strings.Repeat("8", 32)
		squat := fixture.register(t, linkRequest(t, squatter, linkproto.TypeRegister, routeID, "", nil))
		control := fixture.register(t, linkRequest(t, owner, linkproto.TypeRegister, routeID,
			fixture.grant(t, owner, routeID, "home-c0ffee", time.Hour), nil))
		requireClosed(t, squat, 2*time.Second)
		fixture.dialClient(t, "home-c0ffee.link.test")
		readControl(t, control, linkproto.TypeOpenStream)
	})
}

func TestSameKeyRegisterReplacesStaleSession(t *testing.T) {
	for _, hosted := range []bool{false, true} {
		name := "token"
		if hosted {
			name = "hosted"
		}
		t.Run(name, func(t *testing.T) {
			fixture := startRelayFixture(t, nil)
			daemon := newDaemonKey(t)
			routeID := strings.Repeat("9", 32)
			label := "studio-0f0f0f"
			grant := ""
			if hosted {
				grant = fixture.grant(t, daemon, routeID, label, time.Hour)
			}
			stale := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID, grant, nil))
			waiting := fixture.dialClient(t, routeID+".link.test")
			readControl(t, stale, linkproto.TypeOpenStream)

			fresh := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID, grant, nil))
			requireClosed(t, stale, 2*time.Second)
			// The stale session's waiting stream fails instead of hanging
			// until the attach timeout.
			requireClosed(t, waiting, time.Second)

			waitForRelayCondition(t, time.Second, func() bool {
				fixture.server.mu.Lock()
				defer fixture.server.mu.Unlock()
				return len(fixture.server.pending) == 0
			})
			if got := fixture.server.Snapshot().ActiveRoutes; got != 1 {
				t.Fatalf("active routes after replace=%d", got)
			}
			serverName := routeID + ".link.test"
			if hosted {
				serverName = label + ".link.test"
			}
			fixture.dialClient(t, serverName)
			open := readControl(t, fresh, linkproto.TypeOpenStream)
			requireClientHello(t, fixture.attach(t, open))

			// The stale session's teardown left the new route intact.
			_ = fresh.Close()
			waitForRelayCondition(t, 2*time.Second, func() bool {
				return fixture.server.Snapshot().ActiveRoutes == 0
			})
			fixture.server.mu.Lock()
			defer fixture.server.mu.Unlock()
			if len(fixture.server.routes) != 0 || len(fixture.server.labels) != 0 {
				t.Fatalf("route state after close routes=%d labels=%d",
					len(fixture.server.routes), len(fixture.server.labels))
			}
		})
	}
}

func TestGrantExpiryClosesSessionAndRenewalExtendsIt(t *testing.T) {
	fixture := startRelayFixture(t, nil)
	daemon := newDaemonKey(t)

	expiring := strings.Repeat("a", 32)
	expiringControl := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, expiring,
		fixture.grant(t, daemon, expiring, "brief-aaaaaa", 400*time.Millisecond), nil))

	renewed := strings.Repeat("b", 32)
	label := "renew-bbbbbb"
	control := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, renewed,
		fixture.grant(t, daemon, renewed, label, 400*time.Millisecond), nil))
	if err := linkproto.WriteMessage(control, linkRequest(t, daemon, linkproto.TypeRenewGrant, renewed,
		fixture.grant(t, daemon, renewed, "wrong-label", time.Hour), nil)); err != nil {
		t.Fatal(err)
	}
	if rejected := readControl(t, control, linkproto.TypeError); rejected.Code != "renew_rejected" {
		t.Fatalf("mismatched renewal=%#v", rejected)
	}
	longer := fixture.grant(t, daemon, renewed, label, time.Hour)
	if err := linkproto.WriteMessage(control, linkRequest(t, daemon, linkproto.TypeRenewGrant, renewed, longer, nil)); err != nil {
		t.Fatal(err)
	}
	ack := readControl(t, control, linkproto.TypeGrantRenewed)
	if ack.ExpiresAtMS < time.Now().Add(50*time.Minute).UnixMilli() {
		t.Fatalf("renewed expiry=%d", ack.ExpiresAtMS)
	}

	requireClosed(t, expiringControl, 2*time.Second)
	waitForRelayCondition(t, time.Second, func() bool {
		return fixture.server.Snapshot().ActiveRoutes == 1
	})
	requireClosed(t, fixture.dialClient(t, "brief-aaaaaa.link.test"), 2*time.Second)

	time.Sleep(200 * time.Millisecond) // past the original grant expiry
	if err := linkproto.WriteMessage(control, linkproto.Message{
		Type:        linkproto.TypePing,
		TimestampMS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	readControl(t, control, linkproto.TypePong)
	fixture.dialClient(t, label+".link.test")
	readControl(t, control, linkproto.TypeOpenStream)
}

func TestPerIPLimitsRejectAndCount(t *testing.T) {
	t.Run("handshakes", func(t *testing.T) {
		fixture := startRelayFixture(t, func(config *Config) {
			config.HandshakeTimeout = 5 * time.Second
			config.MaxClientHandshakesPerIP = 1
			config.MaxConnectorHandshakesPerIP = 1
		})
		for _, target := range []struct {
			address string
			reason  string
		}{
			{fixture.clientAddr, "client_handshakes_per_ip"},
			{fixture.connectorAddr, "connector_handshakes_per_ip"},
		} {
			held, err := net.Dial("tcp", target.address)
			if err != nil {
				t.Fatal(err)
			}
			waitForRelayCondition(t, time.Second, func() bool {
				fixture.server.ipMu.Lock()
				defer fixture.server.ipMu.Unlock()
				return len(fixture.server.ipUsage) == 1
			})
			rejected, err := net.Dial("tcp", target.address)
			if err != nil {
				t.Fatal(err)
			}
			requireClosed(t, rejected, time.Second)
			if got := fixture.server.capacityRejects[target.reason].Load(); got != 1 {
				t.Fatalf("%s rejections=%d", target.reason, got)
			}
			metrics := scrapeRelayMetrics(t, fixture.server)
			for _, expected := range []string{
				`mewla_link_capacity_used{resource="` + target.reason + `"} 1`,
				`mewla_link_capacity_limit{resource="` + target.reason + `"} 1`,
				`mewla_link_capacity_rejections_total{reason="` + target.reason + `"} 1`,
			} {
				if !strings.Contains(metrics, expected) {
					t.Fatalf("metrics missing %q:\n%s", expected, metrics)
				}
			}
			if strings.Contains(metrics, "127.0.0.1") {
				t.Fatal("metrics leaked a source address")
			}
			_ = held.Close()
			waitForRelayCondition(t, 2*time.Second, func() bool {
				fixture.server.ipMu.Lock()
				defer fixture.server.ipMu.Unlock()
				return len(fixture.server.ipUsage) == 0
			})
		}
	})

	t.Run("client streams", func(t *testing.T) {
		fixture := startRelayFixture(t, func(config *Config) {
			config.MaxClientsPerIP = 1
		})
		daemon := newDaemonKey(t)
		routeID := strings.Repeat("c", 32)
		control := fixture.register(t, linkRequest(t, daemon, linkproto.TypeRegister, routeID, "", nil))
		fixture.dialClient(t, routeID+".link.test")
		readControl(t, control, linkproto.TypeOpenStream)
		requireClosed(t, fixture.dialClient(t, routeID+".link.test"), 2*time.Second)
		if got := fixture.server.capacityRejects["clients_per_ip"].Load(); got != 1 {
			t.Fatalf("clients_per_ip rejections=%d", got)
		}
		if got := fixture.server.Snapshot().ActiveClients; got != 1 {
			t.Fatalf("active clients=%d", got)
		}
	})

	t.Run("ipv6 prefix", func(t *testing.T) {
		first := sourceIPKey(&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2::1"), Port: 1})
		second := sourceIPKey(&net.TCPAddr{IP: net.ParseIP("2001:db8:1:2:ffff::9"), Port: 2})
		third := sourceIPKey(&net.TCPAddr{IP: net.ParseIP("2001:db8:1:3::1"), Port: 3})
		mapped := sourceIPKey(&net.TCPAddr{IP: net.ParseIP("::ffff:192.0.2.7"), Port: 4})
		if first != second || first == third || mapped != "192.0.2.7" {
			t.Fatalf("source keys first=%q second=%q third=%q mapped=%q", first, second, third, mapped)
		}
	})
}

func TestStaticSNIRoutePipesTLSToBackend(t *testing.T) {
	const host = "accounts.example.test"
	backend, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{selfSignedCertificate(t, host)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				request := make([]byte, 4)
				if _, err := io.ReadFull(conn, request); err == nil {
					_, _ = conn.Write(append([]byte("echo:"), request...))
				}
			}()
		}
	}()
	fixture := startRelayFixture(t, func(config *Config) {
		config.StaticRoutes = map[string]string{"Accounts.Example.Test.": backend.Addr().String()}
	})

	raw, err := net.Dial("tcp", fixture.clientAddr)
	if err != nil {
		t.Fatal(err)
	}
	client := tls.Client(raw, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		ServerName:         host,
		InsecureSkipVerify: true,
	})
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("static route handshake/write: %v", err)
	}
	response := make([]byte, len("echo:ping"))
	if _, err := io.ReadFull(client, response); err != nil || string(response) != "echo:ping" {
		t.Fatalf("static route response=%q err=%v", response, err)
	}
	if got := fixture.server.Snapshot().AcceptedClients; got != 1 {
		t.Fatalf("accepted clients=%d", got)
	}
	// A subdomain of the static host does not match.
	requireClosed(t, fixture.dialClient(t, "x."+host), 2*time.Second)
}

func TestStaticSNIRouteSendsTheClientAddress(t *testing.T) {
	const host = "accounts.example.test"
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	backend := tls.NewListener(&proxyproto.Listener{Listener: raw, Timeout: 2 * time.Second}, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{selfSignedCertificate(t, host)},
	})
	remotes := make(chan string, 1)
	go func() {
		conn, err := backend.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		request := make([]byte, 4)
		if _, err := io.ReadFull(conn, request); err == nil {
			remotes <- conn.RemoteAddr().String()
			_, _ = conn.Write([]byte("done"))
		}
	}()
	fixture := startRelayFixture(t, func(config *Config) {
		config.StaticRoutes = map[string]string{host: raw.Addr().String()}
		config.StaticRouteProxyProtocol = true
	})

	conn, err := net.Dial("tcp", fixture.clientAddr)
	if err != nil {
		t.Fatal(err)
	}
	client := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS13, ServerName: host, InsecureSkipVerify: true})
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("handshake/write through the PROXY header: %v", err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(client, response); err != nil || string(response) != "done" {
		t.Fatalf("response=%q err=%v", response, err)
	}
	if got, want := <-remotes, conn.LocalAddr().String(); got != want {
		t.Fatalf("backend saw %s, want the client %s", got, want)
	}
}

func TestRequestPathsDoNotSweepButRejectExpiredState(t *testing.T) {
	server := limitedRelay(t)
	server.config.MaxNonces = 64
	server.config.MaxAdmissions = 64
	server.config.MaxAdmissionsPerRoute = 64
	manager := newDaemonKey(t)
	routeID := strings.Repeat("a", 32)
	control, peer := net.Pipe()
	defer control.Close()
	defer peer.Close()
	server.routes[routeID] = &routeSession{
		routeID:         routeID,
		daemonPublicKey: manager.PublicKeyHex(),
		conn:            control,
	}
	stale := time.Now().Add(-time.Hour)
	server.mu.Lock()
	for index := range 16 {
		server.nonces[strings.Repeat("0", 60)+integerKey(index)] = stale
	}
	expiredAlias := strings.Repeat("e", 32)
	server.admissions[expiredAlias] = admission{routeID: routeID, expiresAt: stale}
	server.admissions[strings.Repeat("f", 32)] = admission{routeID: routeID, expiresAt: stale}
	server.admissionCounts[routeID] = 2
	server.mu.Unlock()

	if err := server.verifySignedRequest(signedRelayRequest(t, manager, linkproto.TypeRegister, time.Now(), "1")); err != nil {
		t.Fatal(err)
	}
	alias, _, err := server.createAdmission(linkproto.Message{
		RouteID:         routeID,
		DaemonPublicKey: manager.PublicKeyHex(),
		TTLSeconds:      30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := server.reserveRoute(strings.Repeat("f", 32), strings.Repeat("1", 32)); ok {
		t.Fatal("expired admission alias was reserved")
	}
	if err := server.consumeAdmission(linkproto.Message{
		RouteID:         routeID,
		DaemonPublicKey: manager.PublicKeyHex(),
		Alias:           expiredAlias,
		StreamID:        strings.Repeat("2", 32),
	}); err == nil {
		t.Fatal("expired admission was consumed")
	}

	server.mu.Lock()
	nonces, admissions, counted := len(server.nonces), len(server.admissions), server.admissionCounts[routeID]
	server.mu.Unlock()
	// Request paths dropped only the two expired admissions they looked up;
	// unrelated stale nonces wait for the sweeper.
	if nonces != 17 || admissions != 1 || counted != 1 {
		t.Fatalf("after requests nonces=%d admissions=%d counted=%d", nonces, admissions, counted)
	}
	if got := server.sweptEntries["nonces"].Load(); got != 0 {
		t.Fatalf("request path swept %d nonces", got)
	}

	server.sweep(time.Now())
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.nonces) != 1 || len(server.admissions) != 1 ||
		server.admissionCounts[routeID] != 1 || server.admissions[alias].routeID != routeID {
		t.Fatalf("after sweep nonces=%d admissions=%d counted=%d",
			len(server.nonces), len(server.admissions), server.admissionCounts[routeID])
	}
	if got := server.sweptEntries["nonces"].Load(); got != 16 {
		t.Fatalf("swept nonces=%d", got)
	}
}

func integerKey(index int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[index/16%16], digits[index%16], '0', '0'})
}

func selfSignedCertificate(t *testing.T, host string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestRelayLogsAreAnonymousAndRateLimited(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	fixture := startRelayFixture(t, func(config *Config) {
		config.MaxClientsPerIP = 1
		config.Logf = func(format string, args ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			mu.Unlock()
		}
	})
	owner := newDaemonKey(t)
	other := newDaemonKey(t)
	routeID := strings.Repeat("e", 32)
	label := "secret-e0e0e0"
	grant := fixture.grant(t, owner, routeID, label, time.Hour)
	fixture.register(t, linkRequest(t, owner, linkproto.TypeRegister, routeID, grant, nil))
	control := fixture.register(t, linkRequest(t, owner, linkproto.TypeRegister, routeID, grant, nil))
	for range 3 {
		fixture.requireRejected(t, linkRequest(t, other, linkproto.TypeRegister, routeID, "", nil),
			"another daemon")
	}
	fixture.dialClient(t, label+".link.test")
	readControl(t, control, linkproto.TypeOpenStream)
	for range 3 {
		requireClosed(t, fixture.dialClient(t, label+".link.test"), 2*time.Second)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	counts := map[string]int{}
	for _, line := range lines {
		for _, event := range []string{
			"route_registered", "route_replaced", "route_rejected", "capacity_rejected",
		} {
			if strings.Contains(line, " "+event+" ") {
				counts[event]++
			}
		}
	}
	for _, event := range []string{"route_registered", "route_replaced", "route_rejected", "capacity_rejected"} {
		if counts[event] != 1 {
			t.Fatalf("%s logged %d times:\n%s", event, counts[event], joined)
		}
	}
	for _, forbidden := range []string{routeID, label, "127.0.0.1", owner.PublicKeyHex(), grant} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("relay log leaked %q:\n%s", forbidden, joined)
		}
	}
}

func TestOneSourceIPHoldsAtMostItsShareOfARoute(t *testing.T) {
	server, err := New(DefaultConfig(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	session := &routeSession{}
	limit := server.routeSourceLimit()
	if limit != server.config.MaxClientsPerRoute/2 {
		t.Fatalf("default share = %d", limit)
	}
	for range limit {
		if !server.reserveRouteSource(session, "203.0.113.9") {
			t.Fatal("source refused within its share")
		}
	}
	if server.reserveRouteSource(session, "203.0.113.9") {
		t.Fatal("one source exceeded its share of the route")
	}
	if !server.reserveRouteSource(session, "198.51.100.7") {
		t.Fatal("another source was locked out of the route")
	}
	server.releaseRouteSource(session, "203.0.113.9")
	if !server.reserveRouteSource(session, "203.0.113.9") {
		t.Fatal("released share was not returned")
	}
	config := DefaultConfig(strings.Repeat("a", 32))
	config.MaxClientsPerRouteIP = config.MaxClientsPerRoute + 1
	if _, err := New(config); err == nil || !strings.Contains(err.Error(), "max-clients-per-route-ip") {
		t.Fatalf("a share above the route cap: err=%v", err)
	}
}

// The connector sees only EOF when the relay drops a control connection, and
// admission rejections were not logged at all; both must leave a line that
// names the cause without aliases, tickets or the route ID itself.
func TestRelayLogsUnansweredControlAndAdmissionRejections(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	fixture := startRelayFixture(t, func(config *Config) {
		config.Logf = func(format string, args ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			mu.Unlock()
		}
	})
	logged := func(prefix string) string {
		mu.Lock()
		defer mu.Unlock()
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) {
				return line
			}
		}
		return ""
	}

	conn, err := net.Dial("tcp", fixture.connectorAddr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for logged("relay control_unanswered") == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if line := logged("relay control_unanswered"); !strings.Contains(line, "cause=closed_by_connector") {
		t.Fatalf("unanswered control line = %q", line)
	}

	manager := newDaemonKey(t)
	routeID := strings.Repeat("ab", 16)
	fixture.requireRejected(t, linkRequest(t, manager, linkproto.TypeAdmissionRequest, routeID, "", func(message *linkproto.Message) {
		message.TTLSeconds = 60
	}), "")
	line := logged("relay admission_rejected")
	if !strings.Contains(line, "step=request route=") || strings.Contains(line, routeID) {
		t.Fatalf("admission rejection line = %q", line)
	}
}
