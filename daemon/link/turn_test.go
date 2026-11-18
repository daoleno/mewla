package link_test

import (
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

// A computer registered with a relay that runs TURN gets credentials for it
// over the signed control exchange; one that is not registered gets none.
func TestConnectorGetsTURNCredentials(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	clientListener := listenLocal(t)
	controlTCP := listenLocal(t)
	controlListener := tls.NewListener(controlTCP, relayTLS)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	turnAddr := udp.LocalAddr().String()
	_ = udp.Close()
	turnServer, err := relay.StartTURN(relay.TURNConfig{PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: turnAddr, URLs: []string{"turn:" + turnAddr + "?transport=udp"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turnServer.Close() })
	relayConfig := relay.DefaultConfig(testConnectorToken)
	relayConfig.TURN = turnServer
	relayServer, err := relay.New(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = relayServer.Serve(ctx, clientListener, controlListener) }()

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
			Name: "local", ControlAddress: controlTCP.Addr().String(), ControlServerName: relayServerName,
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
	if len(creds.URLs) != 1 || !strings.HasSuffix(creds.Username, ":"+identity.RouteID) || creds.Password == "" || time.Until(creds.ExpiresAt) < time.Hour {
		t.Fatalf("credentials %+v", creds)
	}
	again, err := connector.TURN(ctx)
	if err != nil || again.Username != creds.Username || again.Password != creds.Password {
		t.Fatalf("kept credentials %+v, %v", again, err)
	}
}
