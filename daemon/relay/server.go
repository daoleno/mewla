package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/proxyproto"
)

const (
	maxClientHelloBytes = 64 << 10
	copyBufferBytes     = 32 << 10

	maxConfiguredRoutes              = 1 << 20
	maxConfiguredClients             = 1 << 20
	maxConfiguredClientsPerRoute     = 1 << 16
	maxConfiguredClientHandshakes    = 1 << 16
	maxConfiguredConnectorHandshakes = 1 << 16
	maxConfiguredAdmissions          = 1 << 20
	maxConfiguredAdmissionsPerRoute  = 1 << 16
	maxConfiguredNonces              = 1 << 22
	maxConfiguredPendingStreams      = 1 << 20
	maxConfiguredRouteBindings       = 1 << 22
	maxConfiguredPerIP               = 1 << 16

	maxConfiguredHandshakeTimeout = time.Minute
	maxConfiguredAttachTimeout    = time.Minute
	maxConfiguredIdleTimeout      = 24 * time.Hour
	maxConfiguredAuthMaxAge       = time.Hour
	maxConfiguredAdmissionTTL     = 24 * time.Hour
	maxConfiguredSweepInterval    = time.Hour

	// A token-mode route stays bound to its daemon key this long after the
	// daemon disconnects, so another token holder cannot take it over.
	routeBindingRetention = 24 * time.Hour
	capacityLogInterval   = 10 * time.Second
)

type Config struct {
	// ConnectorToken authenticates self-hosted daemons (shared-token mode).
	ConnectorToken string
	// GrantKeys verify hosted route grants signed by the account service.
	// At least one of ConnectorToken and GrantKeys is required.
	GrantKeys []ed25519.PublicKey
	// StaticRoutes pipes client connections whose full SNI equals a key to
	// the TCP address value, letting another TLS service share the port.
	StaticRoutes map[string]string
	// StaticRouteProxyProtocol sends a PROXY protocol v1 header with the
	// client's address before its bytes on every static route.
	StaticRouteProxyProtocol bool
	// Logf receives anonymous operational events. Nil disables logging.
	Logf func(format string, args ...any)

	MaxRoutes          int
	MaxClients         int
	MaxClientsPerRoute int
	// MaxClientsPerRouteIP caps one source IP's share of a route's clients,
	// so one address cannot fill a computer's slots and lock out its own
	// browsers and phones. Zero means half of MaxClientsPerRoute.
	MaxClientsPerRouteIP   int
	MaxClientHandshakes    int
	MaxConnectorHandshakes int
	MaxAdmissions          int
	MaxAdmissionsPerRoute  int
	MaxNonces              int
	MaxPendingStreams      int
	MaxRouteBindings       int

	MaxClientHandshakesPerIP    int
	MaxClientsPerIP             int
	MaxConnectorHandshakesPerIP int

	HandshakeTimeout time.Duration
	AttachTimeout    time.Duration
	IdleTimeout      time.Duration
	AuthMaxAge       time.Duration
	MaxAdmissionTTL  time.Duration
	SweepInterval    time.Duration
}

type Snapshot struct {
	ActiveRoutes    int64
	ActiveClients   int64
	Admissions      int64
	Nonces          int64
	PendingStreams  int64
	AcceptedClients uint64
	RejectedClients uint64
	ForwardedBytes  uint64
}

type Server struct {
	config Config

	mu                  sync.Mutex
	routes              map[string]*routeSession
	labels              map[string]string
	bindings            map[string]routeBinding
	admissions          map[string]admission
	admissionCounts     map[string]int
	pending             map[string]*pendingStream
	nonces              map[string]time.Time
	acceptedConnections map[net.Conn]struct{}
	closing             bool

	ipMu    sync.Mutex
	ipUsage map[string]*ipUsage

	activeRoutes    atomic.Int64
	activeClients   atomic.Int64
	acceptedClients atomic.Uint64
	rejectedClients atomic.Uint64
	forwardedBytes  atomic.Uint64
	capacityRejects map[string]*atomic.Uint64
	rejectionCounts map[string]*atomic.Uint64
	sweptEntries    map[string]*atomic.Uint64
	capacityLogged  map[string]*atomic.Int64
	rejectionLogged atomic.Int64

	listenersMu sync.Mutex
	listeners   []net.Listener

	clientHandshakes    chan struct{}
	connectorHandshakes chan struct{}
	connections         sync.WaitGroup
}

type routeSession struct {
	routeID         string
	daemonPublicKey string
	conn            net.Conn
	writeMu         sync.Mutex
	activeClients   int
	// clientsBySource counts active clients per source IP key.
	clientsBySource map[string]int

	// Hosted sessions carry the label and expiry of their route grant.
	hosted      bool
	label       string
	expiresAt   time.Time
	expiryTimer *time.Timer
}

type routeBinding struct {
	daemonPublicKey string
	// expiresAt is zero while the route is connected.
	expiresAt time.Time
}

type admission struct {
	routeID        string
	expiresAt      time.Time
	reservedStream string
}

type pendingStream struct {
	routeID   string
	session   *routeSession
	ticket    string
	connCh    chan net.Conn
	done      chan struct{}
	expiresAt time.Time
	doneOnce  sync.Once
}

var capacityReasons = []string{
	"routes",
	"clients",
	"clients_per_route",
	"clients_per_route_ip",
	"client_handshakes",
	"connector_handshakes",
	"admissions",
	"admissions_per_route",
	"nonces",
	"pending",
	"route_bindings",
	"client_handshakes_per_ip",
	"clients_per_ip",
	"connector_handshakes_per_ip",
}

var sweptKinds = []string{"admissions", "nonces", "pending", "route_bindings"}

type ipLimit int

const (
	ipClientHandshakes ipLimit = iota
	ipClients
	ipConnectorHandshakes
	ipLimitCount
)

var ipLimitReasons = [ipLimitCount]string{
	"client_handshakes_per_ip",
	"clients_per_ip",
	"connector_handshakes_per_ip",
}

type ipUsage [ipLimitCount]int

const (
	rejectionAuth     = "auth"
	rejectionReplay   = "replay"
	rejectionProtocol = "protocol"
	rejectionCapacity = "capacity"
	rejectionTicket   = "ticket"
	rejectionShutdown = "shutdown"
)

var rejectionReasons = []string{
	rejectionAuth,
	rejectionReplay,
	rejectionProtocol,
	rejectionCapacity,
	rejectionTicket,
	rejectionShutdown,
}

type relayRejectionError struct {
	reason  string
	message string
}

func (err *relayRejectionError) Error() string {
	return err.message
}

type connectionOutcome struct {
	server *Server
	once   sync.Once
}

func DefaultConfig(connectorToken string) Config {
	return Config{
		ConnectorToken:         connectorToken,
		MaxRoutes:              16384,
		MaxClients:             4096,
		MaxClientsPerRoute:     32,
		MaxClientHandshakes:    8192,
		MaxConnectorHandshakes: 1024,
		MaxAdmissions:          65536,
		MaxAdmissionsPerRoute:  16,
		MaxNonces:              262144,
		MaxPendingStreams:      4096,
		MaxRouteBindings:       65536,
		// Browsers open roughly 6-12 parallel connections per origin, and
		// carrier NAT shares one address among many users.
		MaxClientHandshakesPerIP:    64,
		MaxClientsPerIP:             64,
		MaxConnectorHandshakesPerIP: 32,
		HandshakeTimeout:            5 * time.Second,
		AttachTimeout:               5 * time.Second,
		IdleTimeout:                 2 * time.Minute,
		AuthMaxAge:                  2 * time.Minute,
		MaxAdmissionTTL:             15 * time.Minute,
		SweepInterval:               30 * time.Second,
	}
}

func New(config Config) (*Server, error) {
	config.ConnectorToken = strings.TrimSpace(config.ConnectorToken)
	if config.ConnectorToken == "" && len(config.GrantKeys) == 0 {
		return nil, errors.New("connector token or grant public key is required")
	}
	if config.ConnectorToken != "" && len(config.ConnectorToken) < 32 {
		return nil, errors.New("connector token must contain at least 32 characters")
	}
	for _, key := range config.GrantKeys {
		if len(key) != ed25519.PublicKeySize {
			return nil, errors.New("grant public keys must be Ed25519 public keys")
		}
	}
	config.GrantKeys = append([]ed25519.PublicKey(nil), config.GrantKeys...)
	staticRoutes, err := normalizeStaticRoutes(config.StaticRoutes)
	if err != nil {
		return nil, err
	}
	config.StaticRoutes = staticRoutes
	if err := validateConfigBounds(config); err != nil {
		return nil, err
	}
	if config.MaxClientsPerRouteIP < 0 || config.MaxClientsPerRouteIP > config.MaxClientsPerRoute {
		return nil, errors.New("max-clients-per-route-ip must be between 0 and max-clients-per-route")
	}
	capacityRejects := make(map[string]*atomic.Uint64, len(capacityReasons))
	for _, reason := range capacityReasons {
		capacityRejects[reason] = &atomic.Uint64{}
	}
	rejectionCounts := make(map[string]*atomic.Uint64, len(rejectionReasons))
	for _, reason := range rejectionReasons {
		rejectionCounts[reason] = &atomic.Uint64{}
	}
	sweptEntries := make(map[string]*atomic.Uint64, len(sweptKinds))
	for _, kind := range sweptKinds {
		sweptEntries[kind] = &atomic.Uint64{}
	}
	capacityLogged := make(map[string]*atomic.Int64, len(capacityReasons))
	for _, reason := range capacityReasons {
		capacityLogged[reason] = &atomic.Int64{}
	}
	return &Server{
		config:              config,
		routes:              make(map[string]*routeSession),
		labels:              make(map[string]string),
		bindings:            make(map[string]routeBinding),
		admissions:          make(map[string]admission),
		admissionCounts:     make(map[string]int),
		ipUsage:             make(map[string]*ipUsage),
		pending:             make(map[string]*pendingStream),
		nonces:              make(map[string]time.Time),
		acceptedConnections: make(map[net.Conn]struct{}),
		clientHandshakes:    make(chan struct{}, config.MaxClientHandshakes),
		connectorHandshakes: make(chan struct{}, config.MaxConnectorHandshakes),
		capacityRejects:     capacityRejects,
		rejectionCounts:     rejectionCounts,
		sweptEntries:        sweptEntries,
		capacityLogged:      capacityLogged,
	}, nil
}

func normalizeStaticRoutes(routes map[string]string) (map[string]string, error) {
	normalized := make(map[string]string, len(routes))
	for host, address := range routes {
		host = normalizeServerName(host)
		address = strings.TrimSpace(address)
		if host == "" || strings.ContainsAny(host, "/\\\x00: ") {
			return nil, errors.New("static SNI route host is invalid")
		}
		if _, port, err := net.SplitHostPort(address); err != nil || port == "" {
			return nil, fmt.Errorf("static SNI route for %s needs host:port", host)
		}
		normalized[host] = address
	}
	return normalized, nil
}

func validateConfigBounds(config Config) error {
	for _, value := range []struct {
		name  string
		value int
		max   int
	}{
		{"max-routes", config.MaxRoutes, maxConfiguredRoutes},
		{"max-clients", config.MaxClients, maxConfiguredClients},
		{
			"max-clients-per-route",
			config.MaxClientsPerRoute,
			maxConfiguredClientsPerRoute,
		},
		{
			"max-client-handshakes",
			config.MaxClientHandshakes,
			maxConfiguredClientHandshakes,
		},
		{
			"max-connector-handshakes",
			config.MaxConnectorHandshakes,
			maxConfiguredConnectorHandshakes,
		},
		{"max-admissions", config.MaxAdmissions, maxConfiguredAdmissions},
		{
			"max-admissions-per-route",
			config.MaxAdmissionsPerRoute,
			maxConfiguredAdmissionsPerRoute,
		},
		{"max-nonces", config.MaxNonces, maxConfiguredNonces},
		{
			"max-pending-streams",
			config.MaxPendingStreams,
			maxConfiguredPendingStreams,
		},
		{
			"max-route-bindings",
			config.MaxRouteBindings,
			maxConfiguredRouteBindings,
		},
		{
			"max-client-handshakes-per-ip",
			config.MaxClientHandshakesPerIP,
			maxConfiguredPerIP,
		},
		{"max-clients-per-ip", config.MaxClientsPerIP, maxConfiguredPerIP},
		{
			"max-connector-handshakes-per-ip",
			config.MaxConnectorHandshakesPerIP,
			maxConfiguredPerIP,
		},
	} {
		if value.value <= 0 || value.value > value.max {
			return fmt.Errorf(
				"%s must be between 1 and %d",
				value.name,
				value.max,
			)
		}
	}
	for _, value := range []struct {
		name  string
		value time.Duration
		min   time.Duration
		max   time.Duration
	}{
		{
			"handshake-timeout",
			config.HandshakeTimeout,
			time.Nanosecond,
			maxConfiguredHandshakeTimeout,
		},
		{
			"attach-timeout",
			config.AttachTimeout,
			time.Nanosecond,
			maxConfiguredAttachTimeout,
		},
		{
			"idle-timeout",
			config.IdleTimeout,
			time.Nanosecond,
			maxConfiguredIdleTimeout,
		},
		{
			"auth-max-age",
			config.AuthMaxAge,
			time.Nanosecond,
			maxConfiguredAuthMaxAge,
		},
		{
			"max-admission-ttl",
			config.MaxAdmissionTTL,
			time.Second,
			maxConfiguredAdmissionTTL,
		},
		{
			"sweep-interval",
			config.SweepInterval,
			time.Nanosecond,
			maxConfiguredSweepInterval,
		},
	} {
		if value.value < value.min || value.value > value.max {
			return fmt.Errorf(
				"%s must be between %s and %s",
				value.name,
				value.min,
				value.max,
			)
		}
	}
	return nil
}

func (server *Server) Serve(
	ctx context.Context,
	clientListener net.Listener,
	connectorListener net.Listener,
) error {
	if clientListener == nil || connectorListener == nil {
		return errors.New("client and connector listeners are required")
	}
	server.rememberListeners(clientListener, connectorListener)
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wait sync.WaitGroup
	errorsCh := make(chan error, 2)
	wait.Add(3)
	go func() {
		defer wait.Done()
		errorsCh <- server.acceptClients(serveCtx, clientListener)
	}()
	go func() {
		defer wait.Done()
		errorsCh <- server.acceptConnectors(serveCtx, connectorListener)
	}()
	go func() {
		defer wait.Done()
		server.runSweeper(serveCtx)
	}()

	select {
	case <-ctx.Done():
		cancel()
		server.shutdown()
		wait.Wait()
		server.connections.Wait()
		server.finalizeShutdown()
		return ctx.Err()
	case err := <-errorsCh:
		cancel()
		server.shutdown()
		wait.Wait()
		server.connections.Wait()
		server.finalizeShutdown()
		if isListenerClosed(err) {
			return nil
		}
		return err
	}
}

func (server *Server) Snapshot() Snapshot {
	server.mu.Lock()
	admissions := int64(len(server.admissions))
	nonces := int64(len(server.nonces))
	pending := int64(len(server.pending))
	server.mu.Unlock()
	return Snapshot{
		ActiveRoutes:    server.activeRoutes.Load(),
		ActiveClients:   server.activeClients.Load(),
		Admissions:      admissions,
		Nonces:          nonces,
		PendingStreams:  pending,
		AcceptedClients: server.acceptedClients.Load(),
		RejectedClients: server.rejectedClients.Load(),
		ForwardedBytes:  server.forwardedBytes.Load(),
	}
}

// OperatorHandler exposes aggregate metadata only. It never includes route
// identifiers, hostname labels, SNI values, source addresses, stream tickets,
// daemon identities, grants, or inner bytes.
func (server *Server) OperatorHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeOperatorJSON(writer, http.StatusOK, map[string]any{
			"status":  "ok",
			"version": linkproto.CurrentVersion,
		})
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		server.mu.Lock()
		ready := !server.closing
		server.mu.Unlock()
		if !ready {
			writeOperatorJSON(writer, http.StatusServiceUnavailable, map[string]any{
				"status": "stopping",
			})
			return
		}
		writeOperatorJSON(writer, http.StatusOK, map[string]any{"status": "ready"})
	})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snapshot := server.Snapshot()
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintf(writer,
			"mewla_link_active_routes %d\n"+
				"mewla_link_active_streams %d\n"+
				"mewla_link_accepted_streams_total %d\n"+
				"mewla_link_rejected_connections_total %d\n"+
				"mewla_link_forwarded_bytes_total %d\n",
			snapshot.ActiveRoutes,
			snapshot.ActiveClients,
			snapshot.AcceptedClients,
			snapshot.RejectedClients,
			snapshot.ForwardedBytes,
		)
		used, limits := server.capacitySnapshot()
		for _, resource := range capacityReasons {
			_, _ = fmt.Fprintf(
				writer,
				"mewla_link_capacity_used{resource=%q} %d\n"+
					"mewla_link_capacity_limit{resource=%q} %d\n",
				resource,
				used[resource],
				resource,
				limits[resource],
			)
		}
		for _, reason := range capacityReasons {
			_, _ = fmt.Fprintf(
				writer,
				"mewla_link_capacity_rejections_total{reason=%q} %d\n",
				reason,
				server.capacityRejects[reason].Load(),
			)
		}
		for _, reason := range rejectionReasons {
			_, _ = fmt.Fprintf(
				writer,
				"mewla_link_rejected_connections_total{reason=%q} %d\n",
				reason,
				server.rejectionCounts[reason].Load(),
			)
		}
		for _, kind := range sweptKinds {
			_, _ = fmt.Fprintf(
				writer,
				"mewla_link_swept_entries_total{kind=%q} %d\n",
				kind,
				server.sweptEntries[kind].Load(),
			)
		}
	})
	return mux
}

func (server *Server) acceptClients(ctx context.Context, listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		if !server.trackAcceptedConnection(conn) {
			server.recordConnectionRejection(rejectionShutdown)
			_ = conn.Close()
			continue
		}
		source := sourceIPKey(conn.RemoteAddr())
		if !server.acquireIP(source, ipClientHandshakes) {
			server.untrackAcceptedConnection(conn)
			server.recordConnectionRejection(rejectionCapacity)
			_ = conn.Close()
			continue
		}
		select {
		case server.clientHandshakes <- struct{}{}:
			server.connections.Add(1)
			go func() {
				defer server.connections.Done()
				defer server.untrackAcceptedConnection(conn)
				var releaseOnce sync.Once
				release := func() {
					releaseOnce.Do(func() {
						<-server.clientHandshakes
						server.releaseIP(source, ipClientHandshakes)
					})
				}
				defer release()
				outcome := &connectionOutcome{server: server}
				defer outcome.reject(rejectionProtocol)
				server.handleClientWithRelease(
					ctx,
					conn,
					release,
					outcome,
				)
			}()
		default:
			server.releaseIP(source, ipClientHandshakes)
			server.untrackAcceptedConnection(conn)
			server.recordConnectionRejection(rejectionCapacity)
			server.recordCapacityRejection("client_handshakes")
			_ = conn.Close()
		}
	}
}

func (server *Server) acceptConnectors(ctx context.Context, listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		if !server.trackAcceptedConnection(conn) {
			server.recordConnectionRejection(rejectionShutdown)
			_ = conn.Close()
			continue
		}
		source := sourceIPKey(conn.RemoteAddr())
		if !server.acquireIP(source, ipConnectorHandshakes) {
			server.untrackAcceptedConnection(conn)
			server.recordConnectionRejection(rejectionCapacity)
			_ = conn.Close()
			continue
		}
		select {
		case server.connectorHandshakes <- struct{}{}:
			server.connections.Add(1)
			go func() {
				defer server.connections.Done()
				defer server.untrackAcceptedConnection(conn)
				var releaseOnce sync.Once
				release := func() {
					releaseOnce.Do(func() {
						<-server.connectorHandshakes
						server.releaseIP(source, ipConnectorHandshakes)
					})
				}
				defer release()
				outcome := &connectionOutcome{server: server}
				defer outcome.reject(rejectionProtocol)
				server.handleConnectorWithRelease(
					ctx,
					conn,
					release,
					outcome,
				)
			}()
		default:
			server.releaseIP(source, ipConnectorHandshakes)
			server.untrackAcceptedConnection(conn)
			server.recordConnectionRejection(rejectionCapacity)
			server.recordCapacityRejection("connector_handshakes")
			_ = conn.Close()
		}
	}
}

func (server *Server) handleConnectorWithRelease(
	ctx context.Context,
	conn net.Conn,
	releaseHandshake func(),
	outcome *connectionOutcome,
) {
	keep := false
	defer func() {
		if !keep {
			_ = conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(server.config.HandshakeTimeout))
	message, err := linkproto.ReadMessage(conn)
	if err != nil {
		outcome.reject(server.contextRejection(ctx, rejectionProtocol))
		return
	}

	switch message.Type {
	case linkproto.TypeRegister:
		grant, err := server.authenticate(message)
		if err != nil {
			reason := rejectionReasonForError(err, rejectionAuth)
			outcome.reject(reason)
			server.logRouteRejected(message.RouteID, reason)
			_ = writeProtocolError(conn, "registration_rejected", err.Error())
			return
		}
		session, err := server.registerRoute(conn, message, grant)
		if err != nil {
			reason := rejectionReasonForError(err, rejectionProtocol)
			outcome.reject(reason)
			server.logRouteRejected(message.RouteID, reason)
			_ = writeProtocolError(conn, "route_conflict", err.Error())
			return
		}
		releaseHandshake()
		registered := linkproto.Message{
			Type:    linkproto.TypeRegistered,
			RouteID: message.RouteID,
		}
		if session.hosted {
			registered.ExpiresAtMS = session.expiresAt.UnixMilli()
		}
		if err := linkproto.WriteMessage(conn, registered); err != nil {
			outcome.reject(rejectionProtocol)
			server.unregisterRoute(session)
			return
		}
		outcome.succeed()
		_ = conn.SetDeadline(time.Time{})
		keep = true
		server.runRouteSession(ctx, session)
	case linkproto.TypeAdmissionRequest:
		if err := server.verifySignedRequest(message); err != nil {
			outcome.reject(rejectionReasonForError(err, rejectionAuth))
			_ = writeProtocolError(conn, "admission_rejected", err.Error())
			return
		}
		alias, expiresAt, err := server.createAdmission(message)
		if err != nil {
			outcome.reject(rejectionReasonForError(err, rejectionTicket))
			_ = writeProtocolError(conn, "admission_rejected", err.Error())
			return
		}
		releaseHandshake()
		if err := linkproto.WriteMessage(conn, linkproto.Message{
			Type:        linkproto.TypeAdmissionResponse,
			RouteID:     message.RouteID,
			Alias:       alias,
			ExpiresAtMS: expiresAt.UnixMilli(),
		}); err != nil {
			outcome.reject(rejectionProtocol)
			return
		}
		outcome.succeed()
	case linkproto.TypeAdmissionConsume:
		if err := server.verifySignedRequest(message); err != nil {
			outcome.reject(rejectionReasonForError(err, rejectionAuth))
			_ = writeProtocolError(conn, "admission_rejected", err.Error())
			return
		}
		if err := server.consumeAdmission(message); err != nil {
			outcome.reject(rejectionReasonForError(err, rejectionTicket))
			_ = writeProtocolError(conn, "admission_rejected", err.Error())
			return
		}
		releaseHandshake()
		if err := linkproto.WriteMessage(conn, linkproto.Message{
			Type:     linkproto.TypeAdmissionConsumed,
			RouteID:  strings.ToLower(message.RouteID),
			Alias:    strings.ToLower(message.Alias),
			StreamID: strings.ToLower(message.StreamID),
		}); err != nil {
			outcome.reject(rejectionProtocol)
			return
		}
		outcome.succeed()
	case linkproto.TypeAttachStream:
		pending, err := server.claimPendingStream(message)
		if err != nil {
			outcome.reject(rejectionReasonForError(err, rejectionTicket))
			_ = writeProtocolError(conn, "attach_rejected", err.Error())
			return
		}
		releaseHandshake()
		if err := linkproto.WriteMessage(conn, linkproto.Message{
			Type:     linkproto.TypeAttached,
			StreamID: message.StreamID,
		}); err != nil {
			outcome.reject(rejectionProtocol)
			return
		}
		_ = conn.SetDeadline(time.Time{})
		select {
		case pending.connCh <- conn:
			outcome.succeed()
		case <-pending.done:
			outcome.reject(server.contextRejection(ctx, rejectionTicket))
			return
		case <-ctx.Done():
			outcome.reject(rejectionShutdown)
			return
		}
		keep = true
	default:
		outcome.reject(rejectionProtocol)
		_ = writeProtocolError(conn, "unsupported_message", "unsupported Link message")
	}
}

func (server *Server) verifySignedRequest(message linkproto.Message) error {
	_, err := server.authenticate(message)
	return err
}

// authenticate verifies the connector credential (shared token or hosted
// route grant), the daemon signature, and replay. It returns the verified
// grant, or nil for a shared-token request.
func (server *Server) authenticate(
	message linkproto.Message,
) (*linkproto.RouteGrant, error) {
	now := time.Now()
	var grant *linkproto.RouteGrant
	switch {
	case len(server.config.GrantKeys) > 0 &&
		strings.TrimSpace(message.Grant) != "":
		verified, err := linkproto.VerifyRouteGrant(
			server.config.GrantKeys,
			message.Grant,
			now,
		)
		if err != nil {
			return nil, newRelayRejection(rejectionAuth, err.Error())
		}
		grant = &verified
	case server.config.ConnectorToken != "" &&
		subtle.ConstantTimeCompare(
			[]byte(strings.TrimSpace(message.ConnectorToken)),
			[]byte(server.config.ConnectorToken),
		) == 1:
	default:
		return nil, newRelayRejection(
			rejectionAuth,
			"connector authentication failed",
		)
	}
	if !linkproto.IsOpaqueID(message.RouteID, 16) ||
		!linkproto.IsOpaqueID(message.Nonce, 16) {
		return nil, newRelayRejection(
			rejectionProtocol,
			"invalid route or nonce",
		)
	}
	publicKey, err := hex.DecodeString(strings.TrimSpace(message.DaemonPublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, newRelayRejection(
			rejectionAuth,
			"invalid daemon public key",
		)
	}
	daemonID := sha256.Sum256(publicKey)
	if subtle.ConstantTimeCompare(
		[]byte(strings.ToLower(strings.TrimSpace(message.DaemonID))),
		[]byte(hex.EncodeToString(daemonID[:])),
	) != 1 {
		return nil, newRelayRejection(
			rejectionAuth,
			"daemon identity does not match public key",
		)
	}
	timestamp := time.UnixMilli(message.TimestampMS)
	if timestamp.Before(now.Add(-server.config.AuthMaxAge)) ||
		timestamp.After(now.Add(server.config.AuthMaxAge)) {
		return nil, newRelayRejection(rejectionAuth, "Link request expired")
	}
	if !auth.VerifyLinkSignature(
		message.DaemonPublicKey,
		linkproto.SignaturePayload(message),
		message.Signature,
	) {
		return nil, newRelayRejection(
			rejectionAuth,
			"invalid daemon Link signature",
		)
	}
	if grant != nil &&
		(grant.RouteID != strings.ToLower(strings.TrimSpace(message.RouteID)) ||
			grant.DaemonPublicKey != hex.EncodeToString(publicKey)) {
		return nil, newRelayRejection(
			rejectionAuth,
			"route grant does not match the route or daemon key",
		)
	}

	nonceKey := strings.ToLower(strings.TrimSpace(message.DaemonID)) +
		":" +
		strings.ToLower(strings.TrimSpace(message.Nonce))
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return nil, newRelayRejection(
			rejectionShutdown,
			"relay is shutting down",
		)
	}
	if seenAt, exists := server.nonces[nonceKey]; exists {
		// A retained nonce older than the replay window is only awaiting the
		// sweeper; requests that old already fail the timestamp check.
		if !server.nonceExpired(seenAt, now) {
			return nil, newRelayRejection(
				rejectionReplay,
				"Link request replayed",
			)
		}
	} else if len(server.nonces) >= server.config.MaxNonces {
		server.recordCapacityRejection("nonces")
		return nil, newRelayRejection(
			rejectionCapacity,
			"relay nonce capacity is exhausted",
		)
	}
	server.nonces[nonceKey] = now
	return grant, nil
}

func (server *Server) nonceExpired(seenAt, now time.Time) bool {
	return now.Sub(seenAt) > server.config.AuthMaxAge*2
}

// registerRoute binds a route to the verified daemon. The same daemon key
// replaces a stale session; another key is rejected unless a verified grant
// takes over a shared-token route.
func (server *Server) registerRoute(
	conn net.Conn,
	message linkproto.Message,
	grant *linkproto.RouteGrant,
) (*routeSession, error) {
	routeID := strings.ToLower(strings.TrimSpace(message.RouteID))
	daemonKey := strings.ToLower(strings.TrimSpace(message.DaemonPublicKey))
	now := time.Now()
	var session, replaced *routeSession
	server.mu.Lock()
	defer func() {
		server.mu.Unlock()
		if session == nil {
			return
		}
		mode := "token"
		if session.hosted {
			mode = "hosted"
		}
		if replaced != nil {
			_ = replaced.conn.Close()
			server.logf("route_replaced route=%s mode=%s", routeTag(routeID), mode)
			return
		}
		server.logf("route_registered route=%s mode=%s", routeTag(routeID), mode)
	}()
	if server.closing {
		return nil, newRelayRejection(
			rejectionShutdown,
			"relay is shutting down",
		)
	}
	existing := server.routes[routeID]
	binding, bound := server.bindings[routeID]
	if bound && binding.expired(now) {
		delete(server.bindings, routeID)
		bound = false
	}
	if existing != nil && existing.daemonPublicKey != daemonKey &&
		(grant == nil || (existing.hosted && !existing.expired(now))) {
		return nil, newRelayRejection(
			rejectionProtocol,
			"route is already connected by another daemon",
		)
	}
	if grant == nil && bound && binding.daemonPublicKey != daemonKey {
		return nil, newRelayRejection(
			rejectionProtocol,
			"route is bound to another daemon",
		)
	}
	if existing == nil && len(server.routes) >= server.config.MaxRoutes {
		server.recordCapacityRejection("routes")
		return nil, newRelayRejection(
			rejectionCapacity,
			"relay route capacity is exhausted",
		)
	}
	// Grants prove ownership themselves, so a full binding table only limits
	// shared-token routes.
	if grant == nil && !bound &&
		len(server.bindings) >= server.config.MaxRouteBindings {
		server.recordCapacityRejection("route_bindings")
		return nil, newRelayRejection(
			rejectionCapacity,
			"relay route binding capacity is exhausted",
		)
	}
	session = &routeSession{
		routeID:         routeID,
		daemonPublicKey: daemonKey,
		conn:            conn,
	}
	if grant != nil {
		session.hosted = true
		session.label = grant.Label
		session.expiresAt = time.UnixMilli(grant.ExpiresAtMS)
	}
	if existing != nil {
		server.detachSessionLocked(existing)
		if existing.label != "" && existing.label != session.label &&
			server.labels[existing.label] == routeID {
			delete(server.labels, existing.label)
		}
		replaced = existing
	} else {
		server.activeRoutes.Add(1)
	}
	server.routes[routeID] = session
	if bound || len(server.bindings) < server.config.MaxRouteBindings {
		server.bindings[routeID] = routeBinding{daemonPublicKey: daemonKey}
	}
	if session.hosted {
		server.labels[session.label] = routeID
		session.expiryTimer = time.AfterFunc(
			time.Until(session.expiresAt),
			func() { server.expireRoute(session) },
		)
	}
	return session, nil
}

// detachSessionLocked stops a session's grant timer and fails the streams
// still waiting for it. Route, label and binding state stay with the caller.
func (server *Server) detachSessionLocked(session *routeSession) {
	if session.expiryTimer != nil {
		session.expiryTimer.Stop()
	}
	for id, pending := range server.pending {
		if pending.session == session {
			delete(server.pending, id)
			pending.closeDone()
		}
	}
}

func (server *Server) expireRoute(session *routeSession) {
	server.mu.Lock()
	expired := server.routes[session.routeID] == session &&
		session.expired(time.Now())
	server.mu.Unlock()
	if expired {
		server.logf("route_expired route=%s", routeTag(session.routeID))
		_ = session.conn.Close()
	}
}

func (server *Server) runRouteSession(ctx context.Context, session *routeSession) {
	defer server.unregisterRoute(session)
	for {
		_ = session.conn.SetReadDeadline(time.Now().Add(server.config.IdleTimeout))
		message, err := linkproto.ReadMessage(session.conn)
		if err != nil {
			return
		}
		switch message.Type {
		case linkproto.TypePing:
			if err := session.write(linkproto.Message{
				Type:        linkproto.TypePong,
				TimestampMS: message.TimestampMS,
			}); err != nil {
				return
			}
		case linkproto.TypeRenewGrant:
			reply := linkproto.Message{Type: linkproto.TypeGrantRenewed}
			expiresAt, err := server.renewGrant(session, message)
			if err == nil {
				reply.ExpiresAtMS = expiresAt.UnixMilli()
			} else {
				reason := rejectionReasonForError(err, rejectionAuth)
				server.logf(
					"grant_renew_rejected route=%s reason=%s",
					routeTag(session.routeID),
					reason,
				)
				reply = linkproto.Message{
					Type:    linkproto.TypeError,
					Code:    "renew_rejected",
					Message: err.Error(),
				}
			}
			if err := session.write(reply); err != nil {
				return
			}
		default:
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (server *Server) renewGrant(
	session *routeSession,
	message linkproto.Message,
) (time.Time, error) {
	grant, err := server.authenticate(message)
	if err != nil {
		return time.Time{}, err
	}
	if grant == nil {
		return time.Time{}, newRelayRejection(
			rejectionAuth,
			"renew_grant requires a route grant",
		)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.routes[session.routeID] != session || !session.hosted ||
		grant.RouteID != session.routeID ||
		grant.DaemonPublicKey != session.daemonPublicKey ||
		grant.Label != session.label {
		return time.Time{}, newRelayRejection(
			rejectionAuth,
			"route grant does not match this route session",
		)
	}
	if expiresAt := time.UnixMilli(grant.ExpiresAtMS); expiresAt.After(session.expiresAt) {
		session.expiresAt = expiresAt
		session.expiryTimer.Reset(time.Until(expiresAt))
	}
	return session.expiresAt, nil
}

func (server *Server) unregisterRoute(session *routeSession) {
	_ = session.conn.Close()
	server.mu.Lock()
	defer server.mu.Unlock()
	server.detachSessionLocked(session)
	if server.routes[session.routeID] != session {
		// Replaced by a newer session that now owns the route state.
		return
	}
	delete(server.routes, session.routeID)
	server.activeRoutes.Add(-1)
	if session.label != "" && server.labels[session.label] == session.routeID {
		delete(server.labels, session.label)
	}
	if binding, ok := server.bindings[session.routeID]; ok &&
		binding.daemonPublicKey == session.daemonPublicKey {
		binding.expiresAt = time.Now().Add(routeBindingRetention)
		server.bindings[session.routeID] = binding
	}
}

func (session *routeSession) write(message linkproto.Message) error {
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	return linkproto.WriteMessage(session.conn, message)
}

func (session *routeSession) expired(now time.Time) bool {
	return session.hosted && !now.Before(session.expiresAt)
}

func (binding routeBinding) expired(now time.Time) bool {
	return !binding.expiresAt.IsZero() && !now.Before(binding.expiresAt)
}

func (server *Server) createAdmission(message linkproto.Message) (string, time.Time, error) {
	routeID := strings.ToLower(message.RouteID)
	now := time.Now()
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return "", time.Time{}, newRelayRejection(
			rejectionShutdown,
			"relay is shutting down",
		)
	}
	session := server.routes[routeID]
	if session == nil ||
		session.daemonPublicKey != strings.ToLower(message.DaemonPublicKey) ||
		session.expired(now) {
		return "", time.Time{}, newRelayRejection(
			rejectionTicket,
			"route is not connected",
		)
	}
	if err := requireSessionGrant(session, message); err != nil {
		return "", time.Time{}, err
	}
	maxTTLSeconds := int64(server.config.MaxAdmissionTTL / time.Second)
	if message.TTLSeconds <= 0 ||
		message.TTLSeconds > maxTTLSeconds {
		return "", time.Time{}, newRelayRejection(
			rejectionProtocol,
			"admission TTL is invalid",
		)
	}
	if server.admissionCounts[routeID] >= server.config.MaxAdmissionsPerRoute {
		server.recordCapacityRejection("admissions_per_route")
		return "", time.Time{}, newRelayRejection(
			rejectionCapacity,
			"relay per-route admission capacity is exhausted",
		)
	}
	if len(server.admissions) >= server.config.MaxAdmissions {
		server.recordCapacityRejection("admissions")
		return "", time.Time{}, newRelayRejection(
			rejectionCapacity,
			"relay admission capacity is exhausted",
		)
	}
	ttl := time.Duration(message.TTLSeconds) * time.Second
	alias, err := linkproto.RandomID(16)
	if err != nil {
		return "", time.Time{}, newRelayRejection(
			rejectionProtocol,
			fmt.Sprintf("generate admission alias: %v", err),
		)
	}
	expiresAt := now.Add(ttl).UTC()
	server.admissions[alias] = admission{routeID: routeID, expiresAt: expiresAt}
	server.admissionCounts[routeID]++
	return alias, expiresAt, nil
}

// requireSessionGrant keeps hosted routes on grant authentication even when
// the relay also accepts the shared token. Callers have already verified any
// grant present on message against its route and daemon key.
func requireSessionGrant(session *routeSession, message linkproto.Message) error {
	if session.hosted && strings.TrimSpace(message.Grant) == "" {
		return newRelayRejection(
			rejectionAuth,
			"hosted route requires a route grant",
		)
	}
	return nil
}

func (server *Server) deleteAdmissionLocked(alias string) {
	value, exists := server.admissions[alias]
	if !exists {
		return
	}
	delete(server.admissions, alias)
	if count := server.admissionCounts[value.routeID]; count > 1 {
		server.admissionCounts[value.routeID] = count - 1
	} else {
		delete(server.admissionCounts, value.routeID)
	}
}

func (server *Server) consumeAdmission(message linkproto.Message) error {
	alias := strings.ToLower(strings.TrimSpace(message.Alias))
	streamID := strings.ToLower(strings.TrimSpace(message.StreamID))
	routeID := strings.ToLower(strings.TrimSpace(message.RouteID))
	if !linkproto.IsOpaqueID(alias, 16) ||
		!linkproto.IsOpaqueID(streamID, 16) {
		return newRelayRejection(
			rejectionTicket,
			"admission reservation is invalid",
		)
	}

	now := time.Now()
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return newRelayRejection(
			rejectionShutdown,
			"relay is shutting down",
		)
	}
	value, exists := server.admissions[alias]
	if exists && !now.Before(value.expiresAt) {
		server.deleteAdmissionLocked(alias)
		server.recordSweptEntry("admissions")
		exists = false
	}
	if !exists ||
		value.routeID != routeID ||
		value.reservedStream != streamID {
		return newRelayRejection(
			rejectionTicket,
			"admission is invalid, expired, or already consumed",
		)
	}
	session := server.routes[routeID]
	if session == nil ||
		session.daemonPublicKey != strings.ToLower(strings.TrimSpace(message.DaemonPublicKey)) {
		return newRelayRejection(
			rejectionTicket,
			"admission daemon or route binding does not match",
		)
	}
	if err := requireSessionGrant(session, message); err != nil {
		return err
	}
	server.deleteAdmissionLocked(alias)
	return nil
}

func (server *Server) claimPendingStream(message linkproto.Message) (*pendingStream, error) {
	if !linkproto.IsOpaqueID(message.StreamID, 16) ||
		!linkproto.IsOpaqueID(message.StreamTicket, 32) {
		return nil, newRelayRejection(
			rejectionTicket,
			"invalid stream attachment",
		)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return nil, newRelayRejection(
			rejectionShutdown,
			"relay is shutting down",
		)
	}
	streamID := strings.ToLower(message.StreamID)
	pending := server.pending[streamID]
	if pending == nil ||
		subtle.ConstantTimeCompare(
			[]byte(strings.ToLower(message.StreamTicket)),
			[]byte(pending.ticket),
		) != 1 {
		return nil, newRelayRejection(
			rejectionTicket,
			"stream ticket is invalid or expired",
		)
	}
	if !time.Now().Before(pending.expiresAt) {
		delete(server.pending, streamID)
		pending.closeDone()
		server.recordSweptEntry("pending")
		return nil, newRelayRejection(
			rejectionTicket,
			"stream ticket is invalid or expired",
		)
	}
	delete(server.pending, streamID)
	return pending, nil
}

func (server *Server) handleClient(ctx context.Context, client net.Conn) {
	outcome := &connectionOutcome{server: server}
	defer outcome.reject(rejectionProtocol)
	server.handleClientWithRelease(ctx, client, func() {}, outcome)
}

func (server *Server) handleClientWithRelease(
	ctx context.Context,
	client net.Conn,
	releaseHandshake func(),
	outcome *connectionOutcome,
) {
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(server.config.HandshakeTimeout))
	serverName, prefix, err := readClientHelloServerName(client)
	if err != nil {
		outcome.reject(server.contextRejection(ctx, rejectionProtocol))
		return
	}
	_ = client.SetReadDeadline(time.Time{})
	source := sourceIPKey(client.RemoteAddr())
	if target, ok := server.config.StaticRoutes[normalizeServerName(serverName)]; ok {
		server.serveStaticRoute(ctx, client, prefix, target, source, releaseHandshake, outcome)
		return
	}

	streamID, err := linkproto.RandomID(16)
	if err != nil {
		outcome.reject(rejectionProtocol)
		return
	}
	session, routeID, admissionAlias, ok := server.reserveRoute(
		firstDNSLabel(serverName),
		streamID,
	)
	if !ok {
		outcome.reject(server.contextRejection(ctx, rejectionTicket))
		return
	}
	if !server.acquireIP(source, ipClients) {
		server.releaseAdmissionReservation(admissionAlias, streamID)
		outcome.reject(server.contextRejection(ctx, rejectionCapacity))
		return
	}
	defer server.releaseIP(source, ipClients)
	if !server.reserveRouteSource(session, source) {
		server.releaseAdmissionReservation(admissionAlias, streamID)
		outcome.reject(server.contextRejection(ctx, rejectionCapacity))
		return
	}
	defer server.releaseRouteSource(session, source)
	if !server.reserveClient(session) {
		server.releaseAdmissionReservation(admissionAlias, streamID)
		outcome.reject(server.contextRejection(ctx, rejectionCapacity))
		return
	}
	defer server.releaseClient(session, admissionAlias, streamID)
	releaseHandshake()

	ticket, err := linkproto.RandomID(32)
	if err != nil {
		outcome.reject(rejectionProtocol)
		return
	}
	pending := &pendingStream{
		routeID:   routeID,
		session:   session,
		ticket:    ticket,
		connCh:    make(chan net.Conn),
		done:      make(chan struct{}),
		expiresAt: time.Now().Add(server.config.AttachTimeout),
	}
	if !server.storePending(streamID, pending) {
		outcome.reject(server.contextRejection(ctx, rejectionCapacity))
		return
	}
	defer pending.closeDone()
	defer server.removePending(streamID, pending)

	if err := session.write(linkproto.Message{
		Type:         linkproto.TypeOpenStream,
		StreamID:     streamID,
		StreamTicket: ticket,
		Alias:        admissionAlias,
		ClientAddr:   clientIP(client.RemoteAddr()),
	}); err != nil {
		outcome.reject(server.contextRejection(ctx, rejectionProtocol))
		return
	}

	timer := time.NewTimer(server.config.AttachTimeout)
	defer timer.Stop()
	var connector net.Conn
	select {
	case connector, ok = <-pending.connCh:
		if !ok || connector == nil {
			outcome.reject(server.contextRejection(ctx, rejectionTicket))
			return
		}
	case <-timer.C:
		outcome.reject(rejectionTicket)
		return
	case <-pending.done:
		outcome.reject(server.contextRejection(ctx, rejectionTicket))
		return
	case <-ctx.Done():
		outcome.reject(rejectionShutdown)
		return
	}
	defer connector.Close()
	if err := writeAll(connector, prefix); err != nil {
		outcome.reject(server.contextRejection(ctx, rejectionProtocol))
		return
	}
	server.acceptedClients.Add(1)
	outcome.succeed()
	server.forward(ctx, client, connector)
}

// serveStaticRoute pipes a connection whose SNI names a configured static
// host, such as the account service sharing the client port.
func (server *Server) serveStaticRoute(
	ctx context.Context,
	client net.Conn,
	prefix []byte,
	target string,
	source string,
	releaseHandshake func(),
	outcome *connectionOutcome,
) {
	if !server.acquireIP(source, ipClients) {
		outcome.reject(server.contextRejection(ctx, rejectionCapacity))
		return
	}
	defer server.releaseIP(source, ipClients)
	if !server.reserveStaticClient() {
		outcome.reject(server.contextRejection(ctx, rejectionCapacity))
		return
	}
	defer server.releaseStaticClient()
	releaseHandshake()

	dialer := net.Dialer{Timeout: server.config.AttachTimeout}
	upstream, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		outcome.reject(server.contextRejection(ctx, rejectionProtocol))
		return
	}
	defer upstream.Close()
	_ = upstream.SetWriteDeadline(time.Now().Add(server.config.HandshakeTimeout))
	if server.config.StaticRouteProxyProtocol {
		prefix = append(proxyproto.Header(client.RemoteAddr(), client.LocalAddr()), prefix...)
	}
	if err := writeAll(upstream, prefix); err != nil {
		outcome.reject(server.contextRejection(ctx, rejectionProtocol))
		return
	}
	server.acceptedClients.Add(1)
	outcome.succeed()
	server.forward(ctx, client, upstream)
}

// reserveRoute resolves the client's first SNI label: a one-time admission
// alias, then a route ID, then a hosted hostname label. Labels always contain
// a hyphen, so they never collide with the 32-hex forms.
func (server *Server) reserveRoute(
	name string,
	streamID string,
) (*routeSession, string, string, bool) {
	now := time.Now()
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return nil, "", "", false
	}

	key := strings.ToLower(name)
	routeID := key
	admissionAlias := ""
	viaLabel := false
	if value, exists := server.admissions[key]; exists {
		if !now.Before(value.expiresAt) {
			server.deleteAdmissionLocked(key)
			server.recordSweptEntry("admissions")
		} else {
			if value.reservedStream != "" {
				return nil, "", "", false
			}
			admissionAlias = key
			value.reservedStream = strings.ToLower(streamID)
			server.admissions[admissionAlias] = value
			routeID = value.routeID
		}
	} else if _, exists := server.routes[key]; !exists {
		if labeled, exists := server.labels[key]; exists {
			routeID = labeled
			viaLabel = true
		}
	}
	session := server.routes[routeID]
	if session == nil || session.expired(now) ||
		(viaLabel && session.label != key) {
		if admissionAlias != "" {
			value := server.admissions[admissionAlias]
			value.reservedStream = ""
			server.admissions[admissionAlias] = value
		}
		return nil, "", "", false
	}
	return session, routeID, admissionAlias, true
}

func (server *Server) releaseAdmissionReservation(alias string, streamID string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.releaseAdmissionReservationLocked(alias, streamID)
}

func (server *Server) releaseAdmissionReservationLocked(alias string, streamID string) {
	value, exists := server.admissions[strings.ToLower(alias)]
	if !exists || value.reservedStream != strings.ToLower(streamID) {
		return
	}
	value.reservedStream = ""
	server.admissions[strings.ToLower(alias)] = value
}

func (server *Server) reserveClient(session *routeSession) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return false
	}
	if server.activeClients.Load() >= int64(server.config.MaxClients) {
		server.recordCapacityRejection("clients")
		return false
	}
	if session.activeClients >= server.config.MaxClientsPerRoute {
		server.recordCapacityRejection("clients_per_route")
		return false
	}
	session.activeClients++
	server.activeClients.Add(1)
	return true
}

func (server *Server) routeSourceLimit() int {
	if server.config.MaxClientsPerRouteIP > 0 {
		return server.config.MaxClientsPerRouteIP
	}
	return max(1, server.config.MaxClientsPerRoute/2)
}

// reserveRouteSource holds one of source's share of the route's clients.
// Streams the daemon closed stay counted until the client closes its side
// too, so without this share one address could hold every slot.
func (server *Server) reserveRouteSource(session *routeSession, source string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if session.clientsBySource[source] >= server.routeSourceLimit() {
		server.recordCapacityRejection("clients_per_route_ip")
		return false
	}
	if session.clientsBySource == nil {
		session.clientsBySource = make(map[string]int)
	}
	session.clientsBySource[source]++
	return true
}

func (server *Server) releaseRouteSource(session *routeSession, source string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if session.clientsBySource[source] <= 1 {
		delete(session.clientsBySource, source)
		return
	}
	session.clientsBySource[source]--
}

func (server *Server) reserveStaticClient() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return false
	}
	if server.activeClients.Load() >= int64(server.config.MaxClients) {
		server.recordCapacityRejection("clients")
		return false
	}
	server.activeClients.Add(1)
	return true
}

func (server *Server) releaseStaticClient() {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.activeClients.Load() > 0 {
		server.activeClients.Add(-1)
	}
}

func (server *Server) releaseClient(session *routeSession, alias, streamID string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	// Returning client capacity and its unconsumed admission is one release.
	// Idle metrics must not overtake reservation cleanup.
	server.releaseAdmissionReservationLocked(alias, streamID)
	if session.activeClients > 0 {
		session.activeClients--
		server.activeClients.Add(-1)
	}
}

func (server *Server) removePending(streamID string, pending *pendingStream) {
	server.mu.Lock()
	if server.pending[streamID] == pending {
		delete(server.pending, streamID)
	}
	server.mu.Unlock()
}

func (server *Server) storePending(
	streamID string,
	pending *pendingStream,
) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	normalizedID := strings.ToLower(streamID)
	if server.closing || server.pending[normalizedID] != nil {
		return false
	}
	if len(server.pending) >= server.config.MaxPendingStreams {
		server.recordCapacityRejection("pending")
		return false
	}
	server.pending[normalizedID] = pending
	return true
}

func (pending *pendingStream) closeDone() {
	pending.doneOnce.Do(func() {
		close(pending.done)
	})
}

func (server *Server) forward(ctx context.Context, left, right net.Conn) {
	activity := newForwardActivity()
	results := make(chan forwardResult, 2)
	go server.copyDirection(ctx, left, right, activity, results)
	go server.copyDirection(ctx, right, left, activity, results)

	closed := false
	closeBoth := func() {
		if closed {
			return
		}
		closed = true
		_ = left.Close()
		_ = right.Close()
	}
	contextDone := ctx.Done()
	completed := 0
	for completed < 2 {
		select {
		case <-contextDone:
			closeBoth()
			contextDone = nil
		case result := <-results:
			completed++
			if result.err != nil || !result.gracefulEOF {
				closeBoth()
			}
		}
	}
	closeBoth()
}

func (server *Server) copyDirection(
	ctx context.Context,
	destination net.Conn,
	source net.Conn,
	activity *forwardActivity,
	result chan<- forwardResult,
) {
	buffer := make([]byte, copyBufferBytes)
	for {
		if err := ctx.Err(); err != nil {
			result <- forwardResult{err: err}
			return
		}
		deadline := forwardDeadline(ctx, activity, server.config.IdleTimeout)
		_ = source.SetReadDeadline(deadline)
		count, readErr := source.Read(buffer)
		if count > 0 {
			activity.mark()
			_, writeErr := server.writeForwardBytes(
				ctx,
				destination,
				buffer[:count],
				activity,
			)
			if writeErr != nil {
				result <- forwardResult{err: writeErr}
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				_ = destination.SetWriteDeadline(
					forwardDeadline(
						ctx,
						activity,
						server.config.IdleTimeout,
					),
				)
				if err := closeWrite(destination); err != nil {
					result <- forwardResult{err: err}
					return
				}
				result <- forwardResult{gracefulEOF: true}
				return
			}
			if isTimeout(readErr) &&
				ctx.Err() == nil &&
				!activity.idle(server.config.IdleTimeout) {
				continue
			}
			if err := ctx.Err(); err != nil {
				readErr = err
			}
			result <- forwardResult{err: readErr}
			return
		}
	}
}

type forwardResult struct {
	gracefulEOF bool
	err         error
}

type forwardActivity struct {
	started      time.Time
	lastProgress atomic.Int64
}

func newForwardActivity() *forwardActivity {
	return &forwardActivity{started: time.Now()}
}

func (activity *forwardActivity) mark() {
	activity.lastProgress.Store(time.Since(activity.started).Nanoseconds())
}

func (activity *forwardActivity) deadline(idleTimeout time.Duration) time.Time {
	elapsedSinceProgress := time.Since(activity.started) -
		time.Duration(activity.lastProgress.Load())
	return time.Now().Add(
		idleTimeout - elapsedSinceProgress,
	)
}

func (activity *forwardActivity) idle(idleTimeout time.Duration) bool {
	return time.Since(activity.started)-
		time.Duration(activity.lastProgress.Load()) >= idleTimeout
}

func forwardDeadline(
	ctx context.Context,
	activity *forwardActivity,
	idleTimeout time.Duration,
) time.Time {
	deadline := activity.deadline(idleTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok &&
		contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func (server *Server) writeForwardBytes(
	ctx context.Context,
	destination net.Conn,
	raw []byte,
	activity *forwardActivity,
) (int, error) {
	total := 0
	for len(raw) > 0 {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		_ = destination.SetWriteDeadline(
			forwardDeadline(ctx, activity, server.config.IdleTimeout),
		)
		written, err := destination.Write(raw)
		if written > 0 {
			total += written
			raw = raw[written:]
			server.forwardedBytes.Add(uint64(written))
			activity.mark()
		}
		if err != nil {
			if isTimeout(err) &&
				ctx.Err() == nil &&
				!activity.idle(server.config.IdleTimeout) {
				continue
			}
			if contextErr := ctx.Err(); contextErr != nil {
				return total, contextErr
			}
			return total, err
		}
		if written <= 0 {
			return total, io.ErrUnexpectedEOF
		}
	}
	return total, nil
}

func closeWrite(conn net.Conn) error {
	if writer, ok := conn.(interface{ CloseWrite() error }); ok {
		return writer.CloseWrite()
	}
	return nil
}

func isTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func (server *Server) runSweeper(ctx context.Context) {
	ticker := time.NewTicker(server.config.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			server.sweep(now)
		case <-ctx.Done():
			return
		}
	}
}

// sweep removes expired control state. It is the only full scan: request
// paths check expiry on lookup so they stay O(1) under the server lock.
func (server *Server) sweep(now time.Time) {
	server.mu.Lock()
	defer server.mu.Unlock()
	for alias, value := range server.admissions {
		if !now.Before(value.expiresAt) {
			server.deleteAdmissionLocked(alias)
			server.recordSweptEntry("admissions")
		}
	}
	for nonce, seenAt := range server.nonces {
		if server.nonceExpired(seenAt, now) {
			delete(server.nonces, nonce)
			server.recordSweptEntry("nonces")
		}
	}
	for streamID, pending := range server.pending {
		if !now.Before(pending.expiresAt) {
			delete(server.pending, streamID)
			pending.closeDone()
			server.recordSweptEntry("pending")
		}
	}
	for routeID, binding := range server.bindings {
		if binding.expired(now) {
			delete(server.bindings, routeID)
			server.recordSweptEntry("route_bindings")
		}
	}
}

func (server *Server) recordCapacityRejection(reason string) {
	counter := server.capacityRejects[reason]
	if counter == nil {
		return
	}
	total := counter.Add(1)
	if server.logAllowed(server.capacityLogged[reason]) {
		server.logf("capacity_rejected reason=%s total=%d", reason, total)
	}
}

func (server *Server) logRouteRejected(routeID, reason string) {
	if server.logAllowed(&server.rejectionLogged) {
		server.logf("route_rejected route=%s reason=%s", routeTag(routeID), reason)
	}
}

// logAllowed rate-limits one log stream so a flood of rejections cannot turn
// into log volume; metrics keep the exact counts.
func (server *Server) logAllowed(last *atomic.Int64) bool {
	if server.config.Logf == nil || last == nil {
		return false
	}
	now := time.Now().UnixNano()
	previous := last.Load()
	return now-previous >= int64(capacityLogInterval) &&
		last.CompareAndSwap(previous, now)
}

func (server *Server) logf(format string, args ...any) {
	if server.config.Logf != nil {
		server.config.Logf("relay "+format, args...)
	}
}

// routeTag is a short correlation tag that does not reveal the route ID.
func routeTag(routeID string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(routeID))))
	return hex.EncodeToString(sum[:4])
}

func (server *Server) ipLimitFor(kind ipLimit) int {
	switch kind {
	case ipClientHandshakes:
		return server.config.MaxClientHandshakesPerIP
	case ipClients:
		return server.config.MaxClientsPerIP
	default:
		return server.config.MaxConnectorHandshakesPerIP
	}
}

func (server *Server) acquireIP(source string, kind ipLimit) bool {
	server.ipMu.Lock()
	usage := server.ipUsage[source]
	if usage != nil && usage[kind] >= server.ipLimitFor(kind) {
		server.ipMu.Unlock()
		server.recordCapacityRejection(ipLimitReasons[kind])
		return false
	}
	if usage == nil {
		usage = &ipUsage{}
		server.ipUsage[source] = usage
	}
	usage[kind]++
	server.ipMu.Unlock()
	return true
}

func (server *Server) releaseIP(source string, kind ipLimit) {
	server.ipMu.Lock()
	defer server.ipMu.Unlock()
	usage := server.ipUsage[source]
	if usage == nil || usage[kind] == 0 {
		return
	}
	usage[kind]--
	if *usage == (ipUsage{}) {
		delete(server.ipUsage, source)
	}
}

// sourceIPKey groups IPv6 sources by /64, the block one subscriber usually
// controls; IPv4 sources are keyed by address.
// clientIP is the client's bare IP for open_stream, or "" when the address
// is not TCP.
func clientIP(address net.Addr) string {
	if tcp, ok := address.(*net.TCPAddr); ok && tcp.IP != nil {
		return tcp.IP.String()
	}
	return ""
}

func sourceIPKey(address net.Addr) string {
	if address == nil {
		return ""
	}
	var ip net.IP
	if tcp, ok := address.(*net.TCPAddr); ok {
		ip = tcp.IP
	} else {
		host, _, err := net.SplitHostPort(address.String())
		if err != nil {
			return address.String()
		}
		if ip = net.ParseIP(host); ip == nil {
			return host
		}
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func newRelayRejection(reason, message string) error {
	return &relayRejectionError{reason: reason, message: message}
}

func rejectionReasonForError(err error, fallback string) string {
	var rejection *relayRejectionError
	if errors.As(err, &rejection) &&
		rejection.reason != "" {
		return rejection.reason
	}
	return fallback
}

func (outcome *connectionOutcome) reject(reason string) {
	if outcome == nil || outcome.server == nil {
		return
	}
	outcome.once.Do(func() {
		outcome.server.recordConnectionRejection(reason)
	})
}

func (outcome *connectionOutcome) succeed() {
	if outcome == nil {
		return
	}
	outcome.once.Do(func() {})
}

func (server *Server) recordConnectionRejection(reason string) {
	if reason == "" {
		reason = rejectionProtocol
	}
	server.rejectedClients.Add(1)
	if counter := server.rejectionCounts[reason]; counter != nil {
		counter.Add(1)
	}
}

func (server *Server) contextRejection(
	ctx context.Context,
	fallback string,
) string {
	if ctx != nil && ctx.Err() != nil {
		return rejectionShutdown
	}
	server.mu.Lock()
	closing := server.closing
	server.mu.Unlock()
	if closing {
		return rejectionShutdown
	}
	return fallback
}

func (server *Server) trackAcceptedConnection(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return false
	}
	server.acceptedConnections[conn] = struct{}{}
	return true
}

func (server *Server) untrackAcceptedConnection(conn net.Conn) {
	server.mu.Lock()
	delete(server.acceptedConnections, conn)
	server.mu.Unlock()
}

func (server *Server) recordSweptEntry(kind string) {
	if counter := server.sweptEntries[kind]; counter != nil {
		counter.Add(1)
	}
}

func (server *Server) capacitySnapshot() (
	map[string]int64,
	map[string]int64,
) {
	server.mu.Lock()
	maxClientsForRoute := 0
	maxClientsForRouteIP := 0
	for _, route := range server.routes {
		if route.activeClients > maxClientsForRoute {
			maxClientsForRoute = route.activeClients
		}
		for _, count := range route.clientsBySource {
			maxClientsForRouteIP = max(maxClientsForRouteIP, count)
		}
	}
	maxAdmissionsForRoute := 0
	for _, count := range server.admissionCounts {
		maxAdmissionsForRoute = max(maxAdmissionsForRoute, count)
	}
	used := map[string]int64{
		"routes":               int64(len(server.routes)),
		"clients":              server.activeClients.Load(),
		"clients_per_route":    int64(maxClientsForRoute),
		"clients_per_route_ip": int64(maxClientsForRouteIP),
		"client_handshakes":    int64(len(server.clientHandshakes)),
		"connector_handshakes": int64(len(server.connectorHandshakes)),
		"admissions":           int64(len(server.admissions)),
		"admissions_per_route": int64(maxAdmissionsForRoute),
		"nonces":               int64(len(server.nonces)),
		"pending":              int64(len(server.pending)),
		"route_bindings":       int64(len(server.bindings)),
	}
	server.mu.Unlock()
	var maxPerIP ipUsage
	server.ipMu.Lock()
	for _, usage := range server.ipUsage {
		for kind := range maxPerIP {
			maxPerIP[kind] = max(maxPerIP[kind], usage[kind])
		}
	}
	server.ipMu.Unlock()
	for kind, reason := range ipLimitReasons {
		used[reason] = int64(maxPerIP[kind])
	}
	limits := map[string]int64{
		"routes":               int64(server.config.MaxRoutes),
		"clients":              int64(server.config.MaxClients),
		"clients_per_route":    int64(server.config.MaxClientsPerRoute),
		"clients_per_route_ip": int64(server.routeSourceLimit()),
		"client_handshakes":    int64(server.config.MaxClientHandshakes),
		"connector_handshakes": int64(server.config.MaxConnectorHandshakes),
		"admissions":           int64(server.config.MaxAdmissions),
		"admissions_per_route": int64(server.config.MaxAdmissionsPerRoute),
		"nonces":               int64(server.config.MaxNonces),
		"pending":              int64(server.config.MaxPendingStreams),
		"route_bindings":       int64(server.config.MaxRouteBindings),
	}
	for kind, reason := range ipLimitReasons {
		limits[reason] = int64(server.ipLimitFor(ipLimit(kind)))
	}
	return used, limits
}

func (server *Server) rememberListeners(listeners ...net.Listener) {
	server.listenersMu.Lock()
	server.listeners = append(server.listeners, listeners...)
	server.listenersMu.Unlock()
}

func (server *Server) shutdown() {
	server.mu.Lock()
	if server.closing {
		server.mu.Unlock()
		return
	}
	server.closing = true
	routes := make([]*routeSession, 0, len(server.routes))
	for _, route := range server.routes {
		if route.expiryTimer != nil {
			route.expiryTimer.Stop()
		}
		routes = append(routes, route)
	}
	pendingStreams := make([]*pendingStream, 0, len(server.pending))
	for _, pending := range server.pending {
		pendingStreams = append(pendingStreams, pending)
	}
	acceptedConnections := make([]net.Conn, 0, len(server.acceptedConnections))
	for conn := range server.acceptedConnections {
		acceptedConnections = append(acceptedConnections, conn)
	}
	server.clearControlStateLocked()
	server.mu.Unlock()

	server.listenersMu.Lock()
	listeners := append([]net.Listener(nil), server.listeners...)
	server.listenersMu.Unlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
	for _, route := range routes {
		_ = route.conn.Close()
	}
	for _, conn := range acceptedConnections {
		_ = conn.Close()
	}
	for _, pending := range pendingStreams {
		pending.closeDone()
	}
}

func (server *Server) finalizeShutdown() {
	server.mu.Lock()
	server.clearControlStateLocked()
	clear(server.acceptedConnections)
	server.activeClients.Store(0)
	server.mu.Unlock()
}

func (server *Server) clearControlStateLocked() {
	clear(server.routes)
	clear(server.labels)
	clear(server.bindings)
	clear(server.admissions)
	clear(server.admissionCounts)
	clear(server.nonces)
	clear(server.pending)
	server.activeRoutes.Store(0)
}

func writeProtocolError(conn net.Conn, code, message string) error {
	return linkproto.WriteMessage(conn, linkproto.Message{
		Type:    linkproto.TypeError,
		Code:    code,
		Message: message,
	})
}

func readClientHelloServerName(conn net.Conn) (string, []byte, error) {
	var prefix bytes.Buffer
	var handshake bytes.Buffer
	for prefix.Len() < maxClientHelloBytes {
		header := make([]byte, 5)
		if _, err := io.ReadFull(conn, header); err != nil {
			return "", nil, err
		}
		recordLength := int(header[3])<<8 | int(header[4])
		if header[0] != 22 || recordLength <= 0 ||
			prefix.Len()+5+recordLength > maxClientHelloBytes {
			return "", nil, errors.New("invalid TLS ClientHello record")
		}
		body := make([]byte, recordLength)
		if _, err := io.ReadFull(conn, body); err != nil {
			return "", nil, err
		}
		prefix.Write(header)
		prefix.Write(body)
		handshake.Write(body)
		if handshake.Len() < 4 {
			continue
		}
		raw := handshake.Bytes()
		if raw[0] != 1 {
			return "", nil, errors.New("first TLS handshake is not ClientHello")
		}
		helloLength := int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
		if helloLength <= 0 || helloLength+4 > maxClientHelloBytes {
			return "", nil, errors.New("invalid TLS ClientHello length")
		}
		if len(raw) < helloLength+4 {
			continue
		}
		serverName, err := parseClientHelloServerName(raw[4 : helloLength+4])
		return serverName, prefix.Bytes(), err
	}
	return "", nil, errors.New("TLS ClientHello exceeds limit")
}

func parseClientHelloServerName(hello []byte) (string, error) {
	offset := 2 + 32
	if len(hello) < offset+1 {
		return "", errors.New("truncated TLS ClientHello")
	}
	sessionLength := int(hello[offset])
	offset += 1 + sessionLength
	if len(hello) < offset+2 {
		return "", errors.New("truncated TLS session")
	}
	cipherLength := int(hello[offset])<<8 | int(hello[offset+1])
	offset += 2 + cipherLength
	if len(hello) < offset+1 {
		return "", errors.New("truncated TLS cipher suites")
	}
	compressionLength := int(hello[offset])
	offset += 1 + compressionLength
	if len(hello) < offset+2 {
		return "", errors.New("missing TLS extensions")
	}
	extensionsLength := int(hello[offset])<<8 | int(hello[offset+1])
	offset += 2
	if extensionsLength < 0 || len(hello) < offset+extensionsLength {
		return "", errors.New("truncated TLS extensions")
	}
	end := offset + extensionsLength
	for offset+4 <= end {
		extensionType := int(hello[offset])<<8 | int(hello[offset+1])
		extensionLength := int(hello[offset+2])<<8 | int(hello[offset+3])
		offset += 4
		if offset+extensionLength > end {
			return "", errors.New("truncated TLS extension")
		}
		if extensionType == 0 {
			return parseServerNameExtension(hello[offset : offset+extensionLength])
		}
		offset += extensionLength
	}
	return "", errors.New("TLS ClientHello has no SNI")
}

func parseServerNameExtension(raw []byte) (string, error) {
	if len(raw) < 2 {
		return "", errors.New("truncated TLS server name extension")
	}
	listLength := int(raw[0])<<8 | int(raw[1])
	if listLength+2 > len(raw) {
		return "", errors.New("truncated TLS server name list")
	}
	offset := 2
	for offset+3 <= listLength+2 {
		nameType := raw[offset]
		nameLength := int(raw[offset+1])<<8 | int(raw[offset+2])
		offset += 3
		if offset+nameLength > len(raw) {
			return "", errors.New("truncated TLS server name")
		}
		if nameType == 0 {
			name := strings.ToLower(strings.TrimSpace(string(raw[offset : offset+nameLength])))
			if name == "" || strings.ContainsAny(name, "/\\\x00") {
				return "", errors.New("invalid TLS server name")
			}
			return name, nil
		}
		offset += nameLength
	}
	return "", errors.New("TLS ClientHello has no host name")
}

func normalizeServerName(serverName string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(serverName)), ".")
}

func firstDNSLabel(serverName string) string {
	name := normalizeServerName(serverName)
	if index := strings.IndexByte(name, '.'); index >= 0 {
		return name[:index]
	}
	return name
}

func writeAll(writer io.Writer, raw []byte) error {
	_, err := writeCount(writer, raw)
	return err
}

func writeCount(writer io.Writer, raw []byte) (int, error) {
	total := 0
	for len(raw) > 0 {
		written, err := writer.Write(raw)
		total += written
		raw = raw[written:]
		if err != nil {
			return total, err
		}
		if written <= 0 {
			return total, io.ErrUnexpectedEOF
		}
	}
	return total, nil
}

func isListenerClosed(err error) bool {
	return err == nil || errors.Is(err, net.ErrClosed)
}

func writeOperatorJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
