package link

import "testing"

// The app checks the route's signature on the same bytes (client/core/link/routeUpgrade.test.ts carries this text).
func TestRouteBindingPayloadIsLineForLine(t *testing.T) {
	got := string(RouteBindingPayload(" AA ", "BB", "CC", "DD", 1700000000000, []PairingCandidate{
		{Name: "region-a", StableURL: "wss://quiet-otter.a.link.test/ws"},
		{Name: " region-b ", StableURL: "wss://quiet-otter.b.link.test:8443/ws"},
	}))
	want := "1\naa\nbb\ncc\ndd\n1700000000000\nregion-a\nwss://quiet-otter.a.link.test/ws\nregion-b\nwss://quiet-otter.b.link.test:8443/ws"
	if got != want {
		t.Fatalf("binding = %q, want %q", got, want)
	}
}
