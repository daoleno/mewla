package link_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/relay"
)

// startTURNRelay serves a relay that issues TURN credentials under secret on
// these listeners until the returned stop.
func startTURNRelay(t *testing.T, clientListener, controlTCP net.Listener, relayTLS *tls.Config, secret []byte) (*relay.Server, func()) {
	t.Helper()
	issuer, err := relay.NewTURNIssuer(secret, []string{"turn:127.0.0.1:3478?transport=udp"}, 0, 6000)
	if err != nil {
		t.Fatal(err)
	}
	relayConfig := relay.DefaultConfig(testConnectorToken)
	relayConfig.TURN = issuer
	relayServer, err := relay.New(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = relayServer.Serve(ctx, clientListener, tls.NewListener(controlTCP, relayTLS))
	}()
	return relayServer, func() {
		cancel()
		_ = clientListener.Close()
		_ = controlTCP.Close()
		<-done
	}
}

// waitRegistered waits until the relay holds the computer's route and the
// connector has taken that registration as its own. The relay records the
// route before the connector hears back, and TURN asks the connector. The
// relay is read first: once it holds the route of this connection, the
// connector's "connected" is that connection's, not one before a restart.
func waitRegistered(t *testing.T, timeout time.Duration, relayServer *relay.Server, connector *link.Connector) {
	t.Helper()
	waitFor(t, timeout, func() bool {
		return relayServer.Snapshot().ActiveRoutes == 1 && connector.State().Phase == "connected"
	})
}

// A computer registered with a relay that issues TURN credentials gets them
// over the signed control exchange (one that is not registered gets none),
// keeps them while more than half their life is left, and drops them when it
// registers again: a relay that restarted may sign with another secret.
func TestConnectorGetsTURNCredentials(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	clientListener := listenLocal(t)
	controlTCP := listenLocal(t)
	clientAddr, controlAddr := clientListener.Addr().String(), controlTCP.Addr().String()
	relayServer, stop := startTURNRelay(t, clientListener, controlTCP, relayTLS, bytes.Repeat([]byte{1}, 32))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if _, err := link.TURN(ctx); err == nil {
		t.Fatal("TURN credentials without a registered connector")
	}
	stateDir := t.TempDir()
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, []string{"link.test"})
	if err != nil {
		t.Fatal(err)
	}
	connector, err := link.NewConnector(link.ConnectorConfig{
		ConnectorToken: testConnectorToken,
		Candidates: []link.RelayCandidate{{
			Name: "local", ControlAddress: controlAddr, ControlServerName: relayServerName,
			ControlRootCAs: relayRoots, ClientDomain: "link.test", ClientPort: portOf(t, clientListener.Addr()),
		}},
	}, authManager, identity, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = connector.Run(ctx) }()
	waitRegistered(t, 3*time.Second, relayServer, connector)

	creds, err := link.TURN(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds.URLs) != 1 || !strings.Contains(creds.Username, ":"+identity.RouteID+":") || creds.Password == "" || time.Until(creds.ExpiresAt) < 6*time.Hour || creds.MaxKbps != 6000 {
		t.Fatalf("credentials %+v", creds)
	}
	// Each call is a fresh credential: one per peer of each view, since a
	// credential holds a few allocations only.
	again, err := connector.TURN(ctx)
	if err != nil || again.Username == creds.Username || again.Password == creds.Password {
		t.Fatalf("a credential handed out twice: %+v, %v", again, err)
	}

	// The relay restarts (with another secret): the computer registers again
	// and asks anew.
	stop()
	clientListener, err = net.Listen("tcp", clientAddr)
	if err != nil {
		t.Fatal(err)
	}
	controlTCP, err = net.Listen("tcp", controlAddr)
	if err != nil {
		t.Fatal(err)
	}
	relayServer, stop = startTURNRelay(t, clientListener, controlTCP, relayTLS, bytes.Repeat([]byte{2}, 32))
	t.Cleanup(stop)
	waitRegistered(t, 10*time.Second, relayServer, connector)
	fresh, err := connector.TURN(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Username == creds.Username || fresh.Password == creds.Password {
		t.Fatal("the credentials of the relay before its restart were kept")
	}
}
