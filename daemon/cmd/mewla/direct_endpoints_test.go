package main

import (
	"net"
	"testing"

	"github.com/daoleno/mewla/daemon/server"
)

func TestDirectEndpointsFollowTheBindAddress(t *testing.T) {
	detected := []privateNetworkAddress{
		{label: "Wi-Fi/LAN", ip: net.ParseIP("192.168.1.20").To4()},
		{label: "Tailscale", ip: net.ParseIP("100.64.0.9").To4()},
	}
	if got := directEndpoints("127.0.0.1:9876", detected); got != nil {
		t.Fatalf("a loopback daemon is relay-only, got %v", got)
	}
	if got := directEndpoints("garbage", detected); got != nil {
		t.Fatalf("an unreadable address publishes nothing, got %v", got)
	}
	all := directEndpoints("0.0.0.0:9876", detected)
	want := []server.DirectEndpoint{
		{Host: "192.168.1.20", Port: 9876, Kind: "lan"},
		{Host: "100.64.0.9", Port: 9876, Kind: "tailscale"},
	}
	if len(all) != 2 || all[0] != want[0] || all[1] != want[1] {
		t.Fatalf("wildcard bind = %v", all)
	}
	one := directEndpoints("192.168.1.20:9000", detected)
	if len(one) != 1 || one[0].Host != "192.168.1.20" || one[0].Port != 9000 {
		t.Fatalf("bound to one address = %v", one)
	}
	// An address this computer does not own is not offered to the phone.
	if got := directEndpoints("203.0.113.5:9876", detected); got != nil {
		t.Fatalf("a public bind address is never offered as direct, got %v", got)
	}
}
