package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/relay"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("mewla-relay: %v", err)
	}
}

func run(args []string) error {
	defaults := relay.DefaultConfig("")
	var clientAddress string
	var controlAddress string
	var operatorAddress string
	var certificateFile string
	var keyFile string
	var tokenEnvironment string
	var maxRoutes int
	var maxClients int
	var maxClientsPerRoute int
	var maxClientsPerRouteIP int
	var maxClientHandshakes int
	var maxConnectorHandshakes int
	var maxAdmissions int
	var maxAdmissionsPerRoute int
	var maxNonces int
	var maxPendingStreams int
	var maxRouteBindings int
	var maxClientHandshakesPerIP int
	var maxClientsPerIP int
	var maxConnectorHandshakesPerIP int
	var grantKeyFile string
	staticRoutes := sniRoutes{}
	var handshakeTimeout time.Duration
	var attachTimeout time.Duration
	var idleTimeout time.Duration
	var sweepInterval time.Duration
	var checkURL string

	flags := flag.NewFlagSet("mewla-relay", flag.ContinueOnError)
	flags.StringVar(&clientAddress, "client-addr", ":443", "raw L4 client listen address")
	flags.StringVar(&controlAddress, "control-addr", ":8443", "TLS connector control listen address")
	flags.StringVar(&operatorAddress, "operator-addr", "127.0.0.1:8080", "health and metrics listen address")
	flags.StringVar(&certificateFile, "tls-cert", "", "PEM certificate for the connector control listener")
	flags.StringVar(&keyFile, "tls-key", "", "PEM private key for the connector control listener")
	flags.StringVar(&tokenEnvironment, "connector-token-env", "MEWLA_LINK_CONNECTOR_TOKEN", "environment variable containing the shared connector token (optional with -grant-public-key-file)")
	flags.StringVar(&grantKeyFile, "grant-public-key-file", "", "file of hex Ed25519 account service keys, one per line, that sign hosted route grants")
	flags.Var(staticRoutes, "sni-route", "host=addr: pipe clients whose SNI is exactly host to the TCP address (repeatable)")
	flags.IntVar(&maxRoutes, "max-routes", defaults.MaxRoutes, "maximum concurrently registered routes")
	flags.IntVar(&maxClients, "max-clients", defaults.MaxClients, "maximum concurrent client streams")
	flags.IntVar(&maxClientsPerRoute, "max-clients-per-route", defaults.MaxClientsPerRoute, "maximum concurrent streams for one route")
	flags.IntVar(&maxClientsPerRouteIP, "max-clients-per-route-ip", 0, "maximum concurrent streams one source IP holds on one route (0: half of -max-clients-per-route)")
	flags.IntVar(&maxClientHandshakes, "max-client-handshakes", defaults.MaxClientHandshakes, "maximum concurrent client routing handshakes")
	flags.IntVar(&maxConnectorHandshakes, "max-connector-handshakes", defaults.MaxConnectorHandshakes, "maximum concurrent connector TLS/auth handshakes")
	flags.IntVar(&maxAdmissions, "max-admissions", defaults.MaxAdmissions, "maximum unexpired one-time admissions")
	flags.IntVar(&maxAdmissionsPerRoute, "max-admissions-per-route", defaults.MaxAdmissionsPerRoute, "maximum unexpired admissions for one route")
	flags.IntVar(&maxNonces, "max-nonces", defaults.MaxNonces, "maximum retained connector replay nonces")
	flags.IntVar(&maxPendingStreams, "max-pending-streams", defaults.MaxPendingStreams, "maximum streams awaiting connector attachment")
	flags.IntVar(&maxRouteBindings, "max-route-bindings", defaults.MaxRouteBindings, "maximum remembered route to daemon key bindings")
	flags.IntVar(&maxClientHandshakesPerIP, "max-client-handshakes-per-ip", defaults.MaxClientHandshakesPerIP, "maximum concurrent client routing handshakes from one source IP (IPv6 /64)")
	flags.IntVar(&maxClientsPerIP, "max-clients-per-ip", defaults.MaxClientsPerIP, "maximum concurrent client streams from one source IP (IPv6 /64)")
	flags.IntVar(&maxConnectorHandshakesPerIP, "max-connector-handshakes-per-ip", defaults.MaxConnectorHandshakesPerIP, "maximum concurrent connector handshakes from one source IP (IPv6 /64)")
	flags.DurationVar(&handshakeTimeout, "handshake-timeout", defaults.HandshakeTimeout, "maximum time to receive routing SNI or connector auth")
	flags.DurationVar(&attachTimeout, "attach-timeout", defaults.AttachTimeout, "maximum time for a connector to attach a stream")
	flags.DurationVar(&idleTimeout, "idle-timeout", defaults.IdleTimeout, "idle timeout only when neither stream direction makes progress")
	flags.DurationVar(&sweepInterval, "sweep-interval", defaults.SweepInterval, "expired control-state cleanup interval")
	flags.StringVar(&checkURL, "check", "", "check an existing relay health URL and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(checkURL) != "" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get(checkURL)
		if err != nil {
			return fmt.Errorf("relay health check: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("relay health check returned %s", response.Status)
		}
		return nil
	}
	var grantKeys []ed25519.PublicKey
	if strings.TrimSpace(grantKeyFile) != "" {
		keys, err := loadGrantKeys(grantKeyFile)
		if err != nil {
			return err
		}
		grantKeys = keys
	}
	connectorToken := strings.TrimSpace(os.Getenv(strings.TrimSpace(tokenEnvironment)))
	if connectorToken == "" && len(grantKeys) == 0 {
		return fmt.Errorf(
			"connector token environment variable %s is empty and no -grant-public-key-file is set",
			tokenEnvironment,
		)
	}
	server, err := relay.New(relay.Config{
		ConnectorToken:         connectorToken,
		GrantKeys:              grantKeys,
		StaticRoutes:           staticRoutes,
		Logf:                   log.Printf,
		MaxRoutes:              maxRoutes,
		MaxClients:             maxClients,
		MaxClientsPerRoute:     maxClientsPerRoute,
		MaxClientsPerRouteIP:   maxClientsPerRouteIP,
		MaxClientHandshakes:    maxClientHandshakes,
		MaxConnectorHandshakes: maxConnectorHandshakes,
		MaxAdmissions:          maxAdmissions,
		MaxAdmissionsPerRoute:  maxAdmissionsPerRoute,
		MaxNonces:              maxNonces,
		MaxPendingStreams:      maxPendingStreams,
		MaxRouteBindings:       maxRouteBindings,

		MaxClientHandshakesPerIP:    maxClientHandshakesPerIP,
		MaxClientsPerIP:             maxClientsPerIP,
		MaxConnectorHandshakesPerIP: maxConnectorHandshakesPerIP,

		HandshakeTimeout: handshakeTimeout,
		AttachTimeout:    attachTimeout,
		IdleTimeout:      idleTimeout,
		AuthMaxAge:       defaults.AuthMaxAge,
		MaxAdmissionTTL:  defaults.MaxAdmissionTTL,
		SweepInterval:    sweepInterval,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(certificateFile) == "" || strings.TrimSpace(keyFile) == "" {
		return errors.New("-tls-cert and -tls-key are required")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return fmt.Errorf("load control TLS identity: %w", err)
	}

	clientListener, err := net.Listen("tcp", clientAddress)
	if err != nil {
		return fmt.Errorf("listen for Link clients: %w", err)
	}
	defer clientListener.Close()
	controlTCP, err := net.Listen("tcp", controlAddress)
	if err != nil {
		return fmt.Errorf("listen for Link connectors: %w", err)
	}
	defer controlTCP.Close()
	controlListener := tls.NewListener(controlTCP, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{linkproto.ControlALPN},
	})

	operatorListener, err := net.Listen("tcp", operatorAddress)
	if err != nil {
		return fmt.Errorf("listen for operator probes: %w", err)
	}
	defer operatorListener.Close()
	operatorServer := &http.Server{
		Handler:           server.OperatorHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 2)
	go func() {
		errorsCh <- server.Serve(ctx, clientListener, controlListener)
	}()
	go func() {
		errorsCh <- operatorServer.Serve(operatorListener)
	}()

	authModes := make([]string, 0, 2)
	if connectorToken != "" {
		authModes = append(authModes, "token")
	}
	if len(grantKeys) > 0 {
		authModes = append(authModes, "hosted")
	}
	log.Printf(
		"mewla-relay ready: client=%s control=%s operator=%s protocol=%d auth=%s grant_keys=%d sni_routes=%d",
		clientListener.Addr(),
		controlTCP.Addr(),
		operatorListener.Addr(),
		linkproto.CurrentVersion,
		strings.Join(authModes, "+"),
		len(grantKeys),
		len(staticRoutes),
	)
	select {
	case <-ctx.Done():
	case err := <-errorsCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) &&
			!errors.Is(err, net.ErrClosed) &&
			!errors.Is(err, context.Canceled) {
			stop()
			return err
		}
	}
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = operatorServer.Shutdown(shutdownContext)
	return nil
}

// sniRoutes collects repeatable -sni-route host=addr flags.
type sniRoutes map[string]string

func (routes sniRoutes) String() string {
	return fmt.Sprintf("%d routes", len(routes))
}

func (routes sniRoutes) Set(value string) error {
	host, address, ok := strings.Cut(value, "=")
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	address = strings.TrimSpace(address)
	if !ok || host == "" || address == "" {
		return errors.New("want host=addr")
	}
	if _, exists := routes[host]; exists {
		return fmt.Errorf("duplicate SNI route for %s", host)
	}
	routes[host] = address
	return nil
}

func loadGrantKeys(path string) ([]ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read grant public key file: %w", err)
	}
	var keys []ed25519.PublicKey
	for number, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, err := hex.DecodeString(line)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf(
				"grant public key file line %d is not a hex Ed25519 public key",
				number+1,
			)
		}
		keys = append(keys, ed25519.PublicKey(key))
	}
	if len(keys) == 0 {
		return nil, errors.New("grant public key file has no keys")
	}
	return keys, nil
}
