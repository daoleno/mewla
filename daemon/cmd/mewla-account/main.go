package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/account"
	"github.com/daoleno/mewla/daemon/account/dns"
	"github.com/daoleno/mewla/daemon/friendsvc"
	"github.com/daoleno/mewla/daemon/pluginreturn"
	"github.com/daoleno/mewla/daemon/proxyproto"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const letsEncryptDirectory = "https://acme-v02.api.letsencrypt.org/directory"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("mewla-account: %v", err)
	}
}

func run(args []string) error {
	var address string
	var databasePath string
	var grantKeyFile string
	var printPublicKey bool
	var publicURL string
	var clientDomain string
	var clientPort int
	var relays relayFlags
	var acmeDirectory string
	var acmeCAFile string
	var grantTTL time.Duration
	var maxMachines int
	var acmeOpsPerHour int
	var gitHubClientID string
	var devFakeLogin bool
	var dnsProvider string
	var challtestsrvURL string
	var cloudflareZoneID string
	var checkURL string
	var tlsAddress string
	var tlsCacheDir string
	var proxyProtocol bool
	var friendsDB string

	flags := flag.NewFlagSet("mewla-account", flag.ContinueOnError)
	flags.StringVar(&address, "addr", ":8080", "HTTP listen address")
	flags.StringVar(&databasePath, "db", "mewla-account.db", "SQLite database path")
	flags.StringVar(&grantKeyFile, "grant-key-file", "grant-key.pem", "Ed25519 route grant signing key (PKCS#8 PEM; created 0600 when missing)")
	flags.BoolVar(&printPublicKey, "print-public-key", false, "print the grant public key hex (for the relay's -grant-public-key-file) and exit")
	flags.StringVar(&publicURL, "public-url", "", "public base URL of this service (service_url)")
	flags.StringVar(&clientDomain, "client-domain", "", "parent domain of machine hostnames (<label>.<client-domain>)")
	flags.IntVar(&clientPort, "client-port", 443, "relay client port advertised to machines")
	flags.Var(&relays, "relay", "relay as name=...,control=host:port,server-name=...,ca-file=path (repeatable)")
	flags.StringVar(&acmeDirectory, "acme-directory", letsEncryptDirectory, "ACME directory URL advertised to machines")
	flags.StringVar(&acmeCAFile, "acme-ca-file", "", "optional PEM root for the ACME directory's HTTPS (development)")
	flags.DurationVar(&grantTTL, "grant-ttl", time.Hour, "route grant lifetime")
	flags.IntVar(&maxMachines, "max-machines-per-account", 10, "maximum machines per account")
	flags.IntVar(&acmeOpsPerHour, "acme-ops-per-hour", 20, "maximum ACME TXT sets (and clears) per machine per hour")
	flags.StringVar(&gitHubClientID, "github-client-id", "", "GitHub OAuth app client id (default $MEWLA_ACCOUNT_GITHUB_CLIENT_ID)")
	flags.BoolVar(&devFakeLogin, "dev-fake-login", false, "DEVELOPMENT ONLY: anyone can sign in as any username at /device")
	flags.StringVar(&dnsProvider, "dns-provider", "", "DNS-01 provider: challtestsrv or cloudflare")
	flags.StringVar(&challtestsrvURL, "challtestsrv-url", "http://127.0.0.1:8055", "pebble-challtestsrv management API URL")
	flags.StringVar(&cloudflareZoneID, "cloudflare-zone-id", "", "Cloudflare zone id of -client-domain (token in $MEWLA_ACCOUNT_CLOUDFLARE_TOKEN)")
	flags.StringVar(&checkURL, "check", "", "check an existing service health URL and exit")
	flags.StringVar(&tlsAddress, "tls-addr", "", "HTTPS listen address for -public-url's host, certificate from Let's Encrypt by TLS-ALPN-01 (off when empty; /healthz stays on -addr)")
	flags.StringVar(&tlsCacheDir, "tls-cache", "", "certificate cache directory for -tls-addr (default: acme/ next to -db)")
	flags.BoolVar(&proxyProtocol, "proxy-protocol", false, "require a PROXY protocol v1 header on -tls-addr connections (only behind the relay's -sni-route-proxy-protocol)")
	flags.StringVar(&friendsDB, "friends-db", "", "SQLite database of Mewla Friends, mounted under "+friendsvc.PathPrefix+" (off when empty; people are the signed-in Mewla accounts)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(checkURL) != "" {
		return checkHealth(checkURL)
	}

	grantKey, err := loadOrCreateGrantKey(grantKeyFile)
	if err != nil {
		return err
	}
	publicKeyHex := hex.EncodeToString(grantKey.Public().(ed25519.PublicKey))
	if printPublicKey {
		fmt.Println(publicKeyHex)
		return nil
	}

	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if parsed, err := url.Parse(publicURL); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New("-public-url must be an absolute http(s) URL")
	}
	relayConfig, err := relays.load()
	if err != nil {
		return err
	}
	acmeCAPEM := ""
	if strings.TrimSpace(acmeCAFile) != "" {
		acmeCAPEM, err = readPEMFile(acmeCAFile)
		if err != nil {
			return fmt.Errorf("-acme-ca-file: %w", err)
		}
	}

	if gitHubClientID == "" {
		gitHubClientID = strings.TrimSpace(os.Getenv("MEWLA_ACCOUNT_GITHUB_CLIENT_ID"))
	}
	var identity account.IdentityProvider
	var deviceHandler http.Handler
	switch {
	case devFakeLogin && gitHubClientID != "":
		return errors.New("choose one identity provider: -dev-fake-login or -github-client-id")
	case devFakeLogin:
		fake := &account.FakeProvider{VerificationURI: publicURL + "/device"}
		identity = fake
		deviceHandler = fake
		log.Printf("WARNING: -dev-fake-login is enabled: ANYONE who can reach %s/device can sign in as ANY username. Never use this in production.", publicURL)
	case gitHubClientID != "":
		identity = &account.GitHubProvider{ClientID: gitHubClientID, VerificationURI: publicURL + "/device"}
		deviceHandler = http.RedirectHandler(account.GitHubDeviceURL, http.StatusFound)
	default:
		return errors.New("an identity provider is required: -github-client-id (or $MEWLA_ACCOUNT_GITHUB_CLIENT_ID) or -dev-fake-login")
	}

	var dnsClient dns.Provider
	var waitTXT func(context.Context, string, string) error
	switch dnsProvider {
	case "challtestsrv":
		dnsClient = &dns.Challtestsrv{BaseURL: challtestsrvURL}
	case "cloudflare":
		token := strings.TrimSpace(os.Getenv("MEWLA_ACCOUNT_CLOUDFLARE_TOKEN"))
		if token == "" || strings.TrimSpace(cloudflareZoneID) == "" {
			return errors.New("cloudflare needs -cloudflare-zone-id and $MEWLA_ACCOUNT_CLOUDFLARE_TOKEN")
		}
		dnsClient = &dns.Cloudflare{ZoneID: strings.TrimSpace(cloudflareZoneID), Token: token}
		waitTXT = (&dns.TXTWaiter{Zone: zoneOf(clientDomain)}).Wait
	default:
		return errors.New("-dns-provider must be challtestsrv or cloudflare")
	}

	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	store, err := account.OpenStore(databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	service, err := account.New(account.Config{
		Store:                 store,
		Identity:              identity,
		DNS:                   dnsClient,
		GrantKey:              grantKey,
		GrantTTL:              grantTTL,
		ClientDomain:          clientDomain,
		ClientPort:            clientPort,
		Relays:                relayConfig,
		ACMEDirectoryURL:      acmeDirectory,
		ACMECAPEM:             acmeCAPEM,
		MaxMachinesPerAccount: maxMachines,
		ACMEOpsPerHour:        acmeOpsPerHour,
		DeviceHandler:         deviceHandler,
		WaitTXT:               waitTXT,
	})
	if err != nil {
		return err
	}

	handler := http.Handler(service.Handler())
	friends, closeFriends, err := friendsHandler(friendsDB, accountDirectory{store})
	if err != nil {
		return err
	}
	defer closeFriends()
	if friends != nil {
		handler = withFriends(handler, friends)
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	var publicServer *http.Server
	var publicListener net.Listener
	if tlsAddress != "" {
		if tlsCacheDir == "" {
			tlsCacheDir = filepath.Join(filepath.Dir(databasePath), "acme")
		}
		publicServer, publicListener, err = listenPublicTLS(tlsAddress, publicURL, tlsCacheDir, proxyProtocol, handler)
		if err != nil {
			return err
		}
		defer publicListener.Close()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 2)
	go func() {
		errorsCh <- server.Serve(listener)
	}()
	if publicServer != nil {
		go func() {
			errorsCh <- publicServer.Serve(publicListener)
		}()
		log.Printf("mewla-account https: addr=%s proxy_protocol=%t cache=%s", publicListener.Addr(), proxyProtocol, tlsCacheDir)
	}
	log.Printf(
		"mewla-account ready: addr=%s public_url=%s client_domain=%s relays=%d identity=%s dns=%s grant_public_key=%s",
		listener.Addr(),
		publicURL,
		clientDomain,
		len(relayConfig),
		identity.Name(),
		dnsProvider,
		publicKeyHex,
	)
	select {
	case <-ctx.Done():
	case err := <-errorsCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
			return err
		}
	}
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownContext)
	if publicServer != nil {
		_ = publicServer.Shutdown(shutdownContext)
	}
	return nil
}

// friendsHandler is Mewla Friends, mounted under friendsvc.PathPrefix. It runs
// only when -friends-db is set. The returned func closes its database.
func friendsHandler(databasePath string, accounts friendsvc.Accounts) (http.Handler, func(), error) {
	none := func() {}
	if strings.TrimSpace(databasePath) == "" {
		return nil, none, nil
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, none, fmt.Errorf("create friends database directory: %w", err)
	}
	store, err := friendsvc.OpenStore(databasePath)
	if err != nil {
		return nil, none, err
	}
	service, err := friendsvc.New(friendsvc.Config{Store: store, Accounts: accounts})
	if err != nil {
		_ = store.Close()
		return nil, none, err
	}
	return service.Handler(), func() { _ = store.Close() }, nil
}

// accountDirectory tells Friends who a signed-in computer is. Friends has no
// sign-in of its own: a person is a Mewla account.
type accountDirectory struct{ store *account.Store }

func (d accountDirectory) AccountForKey(ctx context.Context, key string) (friendsvc.Account, bool, error) {
	identity, ok, err := d.store.IdentityForKey(ctx, key)
	if err != nil || !ok {
		return friendsvc.Account{}, false, err
	}
	// The age that counts is the provider account's when it is known, and
	// otherwise how long the Mewla account has existed.
	created := identity.CreatedAt
	if !identity.ProviderCreatedAt.IsZero() {
		created = identity.ProviderCreatedAt
	}
	return friendsvc.Account{ID: identity.ID, Login: identity.Login, CreatedAt: created, Disabled: identity.Disabled}, true, nil
}

// withFriends serves Friends under its prefix and everything else as before.
func withFriends(base, friends http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(friendsvc.PathPrefix+"/", friends)
	mux.Handle("/", base)
	return mux
}

// listenPublicTLS serves handler over HTTPS for publicURL's host, with a
// Let's Encrypt certificate by TLS-ALPN-01. Behind the relay that is the
// only challenge that reaches it: the relay passes the TLS stream through.
// Health checks are not served here; the plugin sign-in return page is.
func listenPublicTLS(address, publicURL, cacheDir string, proxyProtocol bool, handler http.Handler) (*http.Server, net.Listener, error) {
	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Scheme != "https" || parsed.Port() != "" {
		return nil, nil, errors.New("-tls-addr needs an https -public-url without a port")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create -tls-cache: %w", err)
	}
	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(parsed.Hostname()),
		Cache:      autocert.DirCache(cacheDir),
	}
	tlsConfig := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: manager.GetCertificate,
		NextProtos:     []string{"http/1.1", acme.ALPNProto},
	}
	raw, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("listen -tls-addr: %w", err)
	}
	listener := raw
	if proxyProtocol {
		listener = &proxyproto.Listener{Listener: raw, Timeout: 5 * time.Second}
	}
	mux := http.NewServeMux()
	mux.Handle("/healthz", http.NotFoundHandler())
	mux.HandleFunc("GET "+pluginreturn.Path, pluginreturn.Handler)
	mux.Handle("/", handler)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	return server, tls.NewListener(listener, tlsConfig), nil
}

// zoneOf returns the registered zone of a client domain such as m.mewla.ai:
// its last two labels. Nameserver checks query that zone's NS records.
func zoneOf(domain string) string {
	labels := strings.Split(strings.Trim(domain, "."), ".")
	if len(labels) > 2 {
		labels = labels[len(labels)-2:]
	}
	return strings.Join(labels, ".")
}

func checkHealth(checkURL string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(checkURL)
	if err != nil {
		return fmt.Errorf("account health check: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("account health check returned %s", response.Status)
	}
	return nil
}

// loadOrCreateGrantKey reads a PKCS#8 PEM Ed25519 key, creating it with mode
// 0600 when the file does not exist. An existing file is never overwritten.
func loadOrCreateGrantKey(path string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("-grant-key-file is required")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return createGrantKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read grant key: %w", err)
	}
	return parseGrantKey(raw)
}

func createGrantKey(path string) (ed25519.PrivateKey, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create grant key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create grant key: %w", err)
	}
	if err := pem.Encode(file, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write grant key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write grant key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("write grant key: %w", err)
	}
	log.Printf("created grant key %s", path)
	return privateKey, nil
}

func parseGrantKey(raw []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("grant key is not a PKCS#8 PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse grant key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("grant key is not Ed25519")
	}
	return privateKey, nil
}

type relayFlag struct {
	name       string
	control    string
	serverName string
	caFile     string
}

type relayFlags []relayFlag

func (r *relayFlags) String() string {
	names := make([]string, 0, len(*r))
	for _, relay := range *r {
		names = append(names, relay.name)
	}
	return strings.Join(names, ",")
}

func (r *relayFlags) Set(value string) error {
	var relay relayFlag
	for _, field := range strings.Split(value, ",") {
		key, fieldValue, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			return fmt.Errorf("relay field %q is not key=value", field)
		}
		fieldValue = strings.TrimSpace(fieldValue)
		switch strings.TrimSpace(key) {
		case "name":
			relay.name = fieldValue
		case "control":
			relay.control = fieldValue
		case "server-name":
			relay.serverName = fieldValue
		case "ca-file":
			relay.caFile = fieldValue
		default:
			return fmt.Errorf("unknown relay field %q", key)
		}
	}
	if relay.name == "" || relay.control == "" {
		return errors.New("relay needs name= and control=")
	}
	if _, _, err := net.SplitHostPort(relay.control); err != nil {
		return fmt.Errorf("relay %s control address: %w", relay.name, err)
	}
	if relay.serverName == "" {
		relay.serverName, _, _ = net.SplitHostPort(relay.control)
	}
	*r = append(*r, relay)
	return nil
}

func (r relayFlags) load() ([]account.Relay, error) {
	if len(r) == 0 {
		return nil, errors.New("at least one -relay is required")
	}
	relays := make([]account.Relay, 0, len(r))
	for _, relay := range r {
		caPEM := ""
		if relay.caFile != "" {
			var err error
			caPEM, err = readPEMFile(relay.caFile)
			if err != nil {
				return nil, fmt.Errorf("relay %s ca-file: %w", relay.name, err)
			}
		}
		relays = append(relays, account.Relay{
			Name:              relay.name,
			ControlAddress:    relay.control,
			ControlServerName: relay.serverName,
			ControlCAPEM:      caPEM,
		})
	}
	return relays, nil
}

func readPEMFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return "", errors.New("no PEM certificates found")
	}
	return string(raw), nil
}
