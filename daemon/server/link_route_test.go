package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

// An app paired by the hosted https address learns the computer's Mewla Relay
// route, signed by the daemon key it pinned, before the direct addresses that
// then apply to it (owner 11-23: existing pairings get the LAN without a new
// pairing).
func TestAppHearsTheSignedRouteBeforeTheDirectAddresses(t *testing.T) {
	authManager, privateKey := enrolledTestDevice(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	route := LinkRoute{
		RouteID:      strings.Repeat("3", 32),
		TransportPin: strings.Repeat("4", 64),
		Candidates:   []link.PairingCandidate{{Name: "main", StableURL: "wss://quiet-otter-3f9a.m.mewla.ai/ws"}},
	}
	srv.SetLinkRoute(func() (LinkRoute, bool) { return route, true })
	srv.SetDirectEndpoints(func() []DirectEndpoint { return []DirectEndpoint{{Host: "192.168.1.20", Port: 9876, Kind: "lan"}} })
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-direct", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var got map[string]any
	for message := range messageSink(t, conn) {
		if message["type"] == "direct_addresses" {
			if got == nil {
				t.Fatal("direct_addresses came before link_route")
			}
			break
		}
		if message["type"] == "link_route" {
			got = message
		}
	}
	if got == nil {
		t.Fatal("no link_route message")
	}
	issued := int64(got["issued_at_ms"].(float64))
	payload := link.RouteBindingPayload(authManager.DaemonID(), authManager.PublicKeyHex(), route.RouteID, route.TransportPin, issued, route.Candidates)
	if !auth.VerifyLinkRouteSignature(authManager.PublicKeyHex(), payload, got["signature"].(string)) {
		t.Fatal("the route's signature does not verify with the daemon key")
	}
	// Another pin under the same signature is refused.
	forged := link.RouteBindingPayload(authManager.DaemonID(), authManager.PublicKeyHex(), route.RouteID, strings.Repeat("5", 64), issued, route.Candidates)
	if auth.VerifyLinkRouteSignature(authManager.PublicKeyHex(), forged, got["signature"].(string)) {
		t.Fatal("a route with another transport pin verified")
	}
}

func TestNoRouteWhileOnNoRelay(t *testing.T) {
	authManager, privateKey := enrolledTestDevice(t)
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetLinkRoute(func() (LinkRoute, bool) { return LinkRoute{}, false })
	srv.SetDirectEndpoints(func() []DirectEndpoint { return nil })
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-direct", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for message := range messageSink(t, conn) {
		if message["type"] == "link_route" {
			t.Fatal("a computer on no relay told a route")
		}
		if message["type"] == "direct_addresses" {
			return
		}
	}
	t.Fatal("no direct_addresses message")
}
