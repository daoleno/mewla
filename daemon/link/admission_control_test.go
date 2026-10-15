package link_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/relay"
)

// A hosted grant may need a refresh from the account service when mewla pair
// asks for an admission. Signing used to happen after dialing, so a refresh
// slower than the relay's handshake timeout made the relay close the control
// connection unanswered and mewla pair reported "main: EOF".
func TestIssueAdmissionsSurvivesAGrantSlowerThanTheRelayHandshake(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	clientListener := listenLocal(t)
	controlTCP := listenLocal(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	relayConfig := relay.DefaultConfig(testConnectorToken)
	relayConfig.HandshakeTimeout = 300 * time.Millisecond
	relayServer, err := relay.New(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = relayServer.Serve(ctx, clientListener, tls.NewListener(controlTCP, relayTLS))
	}()

	stateDir := t.TempDir()
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, []string{"link.test"})
	if err != nil {
		t.Fatal(err)
	}
	var slowGrant atomic.Bool
	config := link.ConnectorConfig{
		ConnectorToken: testConnectorToken,
		Grant: func(context.Context) (string, error) {
			if slowGrant.Load() {
				time.Sleep(700 * time.Millisecond)
			}
			return "", nil
		},
		Candidates: []link.RelayCandidate{{
			Name:              "main",
			ControlAddress:    controlTCP.Addr().String(),
			ControlServerName: relayServerName,
			ControlRootCAs:    relayRoots,
			ClientDomain:      "link.test",
			ClientPort:        portOf(t, clientListener.Addr()),
		}},
	}
	connector, err := link.NewConnector(config, authManager, identity, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = connector.Run(ctx) }()
	waitFor(t, 3*time.Second, func() bool {
		return relayServer.Snapshot().ActiveRoutes == 1
	})

	slowGrant.Store(true)
	offers, err := link.IssueAdmissions(context.Background(), config, authManager, identity, time.Minute)
	if err != nil {
		t.Fatalf("IssueAdmissions with a slow grant: %v", err)
	}
	if len(offers) != 1 || offers[0].AdmissionHost == "" {
		t.Fatalf("offers = %#v", offers)
	}
}

// A relay that hangs up unanswered must say so, with the time it took, not a
// bare EOF.
func TestIssueAdmissionsNamesAnUnansweredRelayClose(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	control := tls.NewListener(listenLocal(t), relayTLS)
	go func() {
		for {
			conn, err := control.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = linkproto.ReadMessage(conn)
				_ = conn.Close()
			}()
		}
	}()

	stateDir := t.TempDir()
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, []string{"link.test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = link.IssueAdmissions(context.Background(), link.ConnectorConfig{
		ConnectorToken: testConnectorToken,
		Candidates: []link.RelayCandidate{{
			Name:              "main",
			ControlAddress:    control.Addr().String(),
			ControlServerName: relayServerName,
			ControlRootCAs:    relayRoots,
			ClientDomain:      "link.test",
			ClientPort:        443,
		}},
	}, authManager, identity, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "main: relay closed the connection without answering after") {
		t.Fatalf("IssueAdmissions error = %v", err)
	}
}
