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
	waitFor(t, 3*time.Second, func() bool { return relayServer.Snapshot().ActiveRoutes == 1 })

	creds, err := link.TURN(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds.URLs) != 1 || !strings.Contains(creds.Username, ":"+identity.RouteID+":") || creds.Password == "" || time.Until(creds.ExpiresAt) < 6*time.Hour || creds.MaxKbps != 6000 {
		t.Fatalf("credentials %+v", creds)
	}
	again, err := connector.TURN(ctx)
	if err != nil || again.Username != creds.Username || again.Password != creds.Password {
		t.Fatalf("kept credentials %+v, %v", again, err)
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
	waitFor(t, 10*time.Second, func() bool { return relayServer.Snapshot().ActiveRoutes == 1 })
	fresh, err := connector.TURN(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Username == creds.Username || fresh.Password == creds.Password {
		t.Fatal("the credentials of the relay before its restart were kept")
	}
}
