package connections

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// A proxy in fake-IP mode (Surge, Clash and others) answers every DNS lookup
// with an address from 198.18.0.0/15 and routes by the name on the connection.
// Mewla must dial it, or every plugin fails on those networks.
func TestCheckedDialerDialsAFakeIPHostname(t *testing.T) {
	var dialled string
	transport := checkedTransportWith("", nil,
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("198.18.0.1")}}, nil
		},
		func(_ context.Context, _, address string) (net.Conn, error) {
			dialled = address
			return nil, errors.New("stub: no network in tests")
		})
	_, err := transport.DialContext(context.Background(), "tcp", "connect.mewla.ai:443")
	if errors.Is(err, errPrivateEndpoint) {
		t.Fatal("a fake-IP answer for a hostname was refused as a private address")
	}
	if dialled != "198.18.0.1:443" {
		t.Fatalf("dialled %q, want 198.18.0.1:443", dialled)
	}
}

// Every other range that can hold a real internal host stays refused, whatever
// the name resolves from, and a literal benchmark address is not a proxy's answer.
func TestCheckedDialerStillRefusesInternalAddresses(t *testing.T) {
	for _, tc := range []struct{ host, ip string }{
		{"evil.example", "10.0.0.5"},
		{"evil.example", "172.16.3.4"},
		{"evil.example", "192.168.1.10"},
		{"evil.example", "127.0.0.1"},
		{"evil.example", "169.254.169.254"},
		{"evil.example", "100.64.0.1"},
		{"evil.example", "192.0.0.8"},
		{"evil.example", "0.0.0.0"},
		{"evil.example", "224.0.0.1"},
		{"evil.example", "::1"},
		{"evil.example", "fd00::1"},
		{"198.18.0.1", "198.18.0.1"},
		{"2001:2::5", "2001:2::5"},
		{"mixed.example", "2001:2::5,10.0.0.5"},
		{"mixed.example", "198.18.0.1,10.0.0.5"},
	} {
		var addresses []net.IPAddr
		for _, ip := range strings.Split(tc.ip, ",") {
			addresses = append(addresses, net.IPAddr{IP: net.ParseIP(ip)})
		}
		dialled := false
		transport := checkedTransportWith("", nil,
			func(context.Context, string) ([]net.IPAddr, error) { return addresses, nil },
			func(context.Context, string, string) (net.Conn, error) {
				dialled = true
				return nil, errors.New("stub")
			})
		_, err := transport.DialContext(context.Background(), "tcp", net.JoinHostPort(tc.host, "443"))
		if !errors.Is(err, errPrivateEndpoint) || dialled {
			t.Errorf("%s -> %s: err %v, dialled %t; want refused", tc.host, tc.ip, err, dialled)
		}
	}
}

func TestExchangeNamesAPrivateAddressAnswer(t *testing.T) {
	wrapped := &net.OpError{Op: "dial", Err: errPrivateEndpoint}
	if got := exchangeFailure(wrapped); got != "this network answers with a private address" {
		t.Fatalf("cause %q", got)
	}
}

// Clash Verge (mihomo) in TUN mode answers one name with both families:
// 198.18.0.12 and 2001:2::5. Neither may poison the other, and both are tried.
func TestCheckedDialerAcceptsBothFakeIPFamilies(t *testing.T) {
	var dialled []string
	transport := checkedTransportWith("", nil,
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("198.18.0.12")}, {IP: net.ParseIP("2001:2::5")}}, nil
		},
		func(_ context.Context, _, address string) (net.Conn, error) {
			dialled = append(dialled, address)
			return nil, errors.New("stub: no network in tests")
		})
	_, err := transport.DialContext(context.Background(), "tcp", "connect.mewla.ai:443")
	if errors.Is(err, errPrivateEndpoint) {
		t.Fatalf("a fake-IP answer set was refused: %v", err)
	}
	if len(dialled) != 2 || dialled[0] != "198.18.0.12:443" || dialled[1] != "[2001:2::5]:443" {
		t.Fatalf("dialled %v", dialled)
	}
}
