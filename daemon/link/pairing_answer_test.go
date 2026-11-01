package link_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/relay"
	daemonserver "github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A one-time pairing is one request on one stream that the computer closes
// right after answering. The answer must reach the phone every time.
func TestPairingAnswerReachesThePhoneEveryTime(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	clientListener := listenLocal(t)
	controlTCP := listenLocal(t)
	controlListener := tls.NewListener(controlTCP, relayTLS)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	relayConfig := relay.DefaultConfig(testConnectorToken)
	relayConfig.MaxClients = 64
	relayConfig.MaxClientsPerRoute = 32
	relayConfig.MaxClientsPerRouteIP = 32
	relayConfig.HandshakeTimeout = 2 * time.Second
	relayConfig.AttachTimeout = 2 * time.Second
	relayConfig.IdleTimeout = time.Minute
	relayServer, err := relay.New(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = relayServer.Serve(ctx, clientListener, controlListener) }()

	stateDir := t.TempDir()
	authManager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, []string{"link.test"})
	if err != nil {
		t.Fatal(err)
	}
	handler := daemonserver.New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil).Handler()
	config := link.ConnectorConfig{
		ConnectorToken: testConnectorToken,
		Candidates: []link.RelayCandidate{{
			Name:              "local",
			ControlAddress:    controlTCP.Addr().String(),
			ControlServerName: relayServerName,
			ControlRootCAs:    relayRoots,
			ClientDomain:      "link.test",
			ClientPort:        portOf(t, clientListener.Addr()),
		}},
	}
	connector, err := link.NewConnector(config, authManager, identity, handler)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = connector.Run(ctx) }()
	waitFor(t, 3*time.Second, func() bool { return relayServer.Snapshot().ActiveRoutes == 1 })

	failures := 0
	const rounds = 25
	for round := 0; round < rounds; round++ {
		offers, err := link.IssueAdmissions(context.Background(), config, authManager, identity, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		pairing, err := authManager.IssuePairingToken(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
		body, _ := json.Marshal(map[string]string{
			"enrollment_token":           pairing.Value,
			"expected_daemon_public_key": authManager.PublicKeyHex(),
			"device_id":                  "00000000-0000-4000-8000-" + hex.EncodeToString(publicKey[:6]),
			"device_name":                "stress",
			"device_public_key":          hex.EncodeToString(publicKey),
		})
		response, text, err := doHTTPThroughLink(
			clientListener.Addr().String(), offers[0].AdmissionHost, identity.SPKISHA256,
			http.MethodPost, "/pair", bytes.NewReader(body),
			map[string]string{"Content-Type": "application/json"},
		)
		if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(text, `"ok":true`) {
			failures++
			t.Logf("round %d: err=%v status=%v body=%q", round, err, func() any {
				if response == nil {
					return nil
				}
				return response.StatusCode
			}(), text)
		}
	}
	if failures > 0 {
		t.Fatalf("%d of %d pairings got no answer", failures, rounds)
	}
}
