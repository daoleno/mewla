package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/account"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/relay"
)

type addressDNS struct {
	mu    sync.Mutex
	hosts map[string]string
}

func (d *addressDNS) SetTXT(context.Context, string, string) error   { return nil }
func (d *addressDNS) ClearTXT(context.Context, string, string) error { return nil }
func (d *addressDNS) SetA(_ context.Context, fqdn, ip string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hosts[fqdn] = ip
	return nil
}
func (d *addressDNS) ClearA(_ context.Context, fqdn string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.hosts, fqdn)
	return nil
}
func (d *addressDNS) lookup(host string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ip, ok := d.hosts[host]
	return ip, ok
}

// slowListener holds each accepted connection for a while, so a daemon measures
// that relay as the far one.
type slowListener struct {
	net.Listener
	delay time.Duration
}

func (l slowListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		time.Sleep(l.delay)
	}
	return conn, err
}

type fakeRelay struct {
	server  *relay.Server
	control string
	cancel  context.CancelFunc
	done    chan struct{}
}

func startFakeRelay(t *testing.T, tlsConfig *tls.Config, grantKey ed25519.PublicKey, delay time.Duration) *fakeRelay {
	t.Helper()
	config := relay.DefaultConfig("")
	config.GrantKeys = []ed25519.PublicKey{grantKey}
	config.AttachTimeout = 2 * time.Second
	server, err := relay.New(config)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	controlTCP, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &fakeRelay{server: server, control: controlTCP.Addr().String(), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		_ = server.Serve(ctx, clients, tls.NewListener(slowListener{controlTCP, delay}, tlsConfig))
	}()
	t.Cleanup(func() { cancel(); <-r.done })
	return r
}

func (r *fakeRelay) stop() { r.cancel(); <-r.done }

func relayControlTLS(t *testing.T) (*tls.Config, string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "relay-control.test"},
		DNSNames: []string{"relay-control.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, NextProtos: []string{linkproto.ControlALPN},
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}},
	}, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func waitUntil(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A computer signed in to a service with two relays registers on the nearer
// one, tells the service, and its hostname follows: to the far relay's address
// while it is there, and back to nothing (the wildcard) when it moves to the
// default relay. New pairings use the relay's own domain.
func TestTwoRelaysADaemonOnTheNearOneIsFoundThereAndFollowsAMove(t *testing.T) {
	tlsConfig, caPEM := relayControlTLS(t)
	grantPublic, grantPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// "main" is the default but slow to answer; "hk" is near.
	main := startFakeRelay(t, tlsConfig, grantPublic, 150*time.Millisecond)
	hk := startFakeRelay(t, tlsConfig, grantPublic, 0)

	store, err := account.OpenStore(t.TempDir() + "/account.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dnsRecords := &addressDNS{hosts: map[string]string{}}
	fake := &account.FakeProvider{Interval: 10 * time.Millisecond}
	service, err := account.New(account.Config{
		Store: store, Identity: fake, DNS: dnsRecords, GrantKey: grantPrivate,
		ClientDomain: "m.test", ClientPort: 443, DeviceHandler: fake,
		Relays: []account.Relay{
			{Name: "main", ControlAddress: main.control, ControlServerName: "relay-control.test", ControlCAPEM: caPEM},
			{Name: "hk", ControlAddress: hk.control, ControlServerName: "relay-control.test", ControlCAPEM: caPEM,
				ClientDomain: "hk.m.test", IP: "203.0.113.9", Feature: account.FeatureRelayDNS},
		},
		Logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	fake.VerificationURI = server.URL + "/device"

	stateDir := t.TempDir()
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(server.URL, "", manager)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, []string{"studio.m.test"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := client.StartLogin(context.Background(), "studio", identity.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := server.Client().PostForm(server.URL+"/device", url.Values{"user_code": {start.UserCode}, "username": {"alice"}}); err != nil {
		t.Fatal(err)
	} else {
		response.Body.Close()
	}
	var machine Machine
	waitUntil(t, "the sign-in to complete", func() bool {
		machine, err = client.PollLogin(context.Background(), start.LoginID)
		return err == nil
	})
	if len(machine.Relays) != 2 || machine.Relays[1].ClientDomain != "hk.m.test" {
		t.Fatalf("a daemon that reports its relay was offered %+v", machine.Relays)
	}
	config := Config{ServiceURL: server.URL, Machine: machine}

	grants := &grantSource{stateDir: stateDir, client: client, config: config}
	connectorConfig, err := connectorConfigFor(config, grants.Grant)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reporter := &relayReporter{ctx: ctx, client: client, logf: t.Logf, backoff: func(int) time.Duration { return 20 * time.Millisecond }}
	connectorConfig.StateObserver = func(state link.ConnectorState) {
		t.Logf("connector: %s relay=%s err=%s", state.Phase, state.Relay, state.LastError)
		if state.Phase == "connected" {
			reporter.report(state.Relay)
		}
	}
	connector, err := link.NewConnector(connectorConfig, manager, identity, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = connector.Run(ctx) }()

	// It registers on the near relay, and only there.
	waitUntil(t, "the daemon to register on hk", func() bool { return hk.server.Snapshot().ActiveRoutes == 1 })
	if n := main.server.Snapshot().ActiveRoutes; n != 0 {
		t.Fatalf("the far relay holds %d routes", n)
	}
	// The service learns it and points the hostname at hk.
	waitUntil(t, "the hostname to point at hk", func() bool {
		ip, ok := dnsRecords.lookup(machine.Hostname)
		return ok && ip == "203.0.113.9"
	})

	// A new pairing goes through hk, under hk's own domain, which a wildcard
	// record for that domain reaches without any per-computer record.
	offers, err := link.IssueAdmissions(ctx, connectorConfig, manager, identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].RelayName != "hk" ||
		!strings.HasSuffix(offers[0].AdmissionHost, ".hk.m.test") ||
		offers[0].StableHost != machine.Label+".hk.m.test" {
		t.Fatalf("offers = %+v: only the relay the computer is on can admit a phone, under its own domain", offers)
	}

	// hk goes away: the daemon re-registers on the default relay and the
	// override disappears, so the wildcard covers the hostname again.
	hk.stop()
	waitUntil(t, "the daemon to move to main", func() bool { return main.server.Snapshot().ActiveRoutes == 1 })
	waitUntil(t, "the override to go away", func() bool {
		_, ok := dnsRecords.lookup(machine.Hostname)
		return !ok
	})
	offers, err = link.IssueAdmissions(ctx, connectorConfig, manager, identity, time.Minute)
	if err != nil || len(offers) != 1 || offers[0].RelayName != "main" ||
		!strings.HasSuffix(offers[0].AdmissionHost, ".m.test") || strings.HasSuffix(offers[0].AdmissionHost, ".hk.m.test") {
		t.Fatalf("after the move: offers = %+v err = %v", offers, err)
	}
}
