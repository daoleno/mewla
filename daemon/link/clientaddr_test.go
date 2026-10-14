package link_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/daoleno/mewla/daemon/link"
)

func TestRelayReportsEachClientAddressToTheHandler(t *testing.T) {
	relayTLS, relayRoots, relayServerName := relayTLSFixture(t)
	fixture := startRelayFixture(t, relayTLS, "", "")
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
			Name:              "local",
			ControlAddress:    fixture.controlAddress,
			ControlServerName: relayServerName,
			ControlRootCAs:    relayRoots,
			ClientDomain:      "link.test",
			ClientPort:        fixture.clientPort,
		}},
	}, authManager, identity, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s", clientaddr.IP(r), r.RemoteAddr)
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = connector.Run(ctx) }()
	waitFor(t, 3*time.Second, func() bool { return fixture.server.Snapshot().ActiveRoutes == 1 })

	for _, source := range []string{"127.0.0.2", "127.0.0.3"} {
		dialer := net.Dialer{Timeout: time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}}
		raw, err := dialer.Dial("tcp", fixture.clientAddress)
		if err != nil {
			// macOS only configures 127.0.0.1 on loopback by default.
			t.Skipf("cannot dial from %s on this host: %v", source, err)
		}
		config, err := link.PinnedClientTLSConfig(identity.RouteID+".link.test", identity.SPKISHA256)
		if err != nil {
			t.Fatal(err)
		}
		config.NextProtos = []string{"http/1.1"}
		conn := tls.Client(raw, config)
		request, _ := http.NewRequest(http.MethodGet, "https://"+identity.RouteID+".link.test/", nil)
		request.Close = true
		if err := request.Write(conn); err != nil {
			t.Fatalf("request from %s: %v", source, err)
		}
		response, err := http.ReadResponse(bufio.NewReader(conn), request)
		if err != nil {
			t.Fatalf("response for %s: %v", source, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = conn.Close()
		var reported, remote string
		fmt.Sscanf(string(body), "%s %s", &reported, &remote)
		if reported != source {
			t.Fatalf("handler saw client %q for a request from %s (RemoteAddr %s)", reported, source, remote)
		}
		if host, _, _ := net.SplitHostPort(remote); host == source {
			t.Fatalf("RemoteAddr %s must stay the relay connection, not the reported client", remote)
		}
	}
}
