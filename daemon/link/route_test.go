package link

import "testing"

// The app checks the route's signature on the same bytes (client/core/link/routeUpgrade.test.ts carries this text).
func TestRouteBindingPayloadIsLineForLine(t *testing.T) {
	payload, err := RouteBindingPayload(" AA ", "BB", "CC", "DD", 1700000000000, []PairingCandidate{
		{Name: "region-a", StableURL: "wss://quiet-otter.a.link.test/ws"},
		{Name: " region-b ", StableURL: "wss://quiet-otter.b.link.test:8443/ws"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "1\naa\nbb\ncc\ndd\n1700000000000\nregion-a\nwss://quiet-otter.a.link.test/ws\nregion-b\nwss://quiet-otter.b.link.test:8443/ws"
	if got := string(payload); got != want {
		t.Fatalf("binding = %q, want %q", got, want)
	}

	// A control character in any name or URL is refused, not signed: a newline would let the same bytes read as
	// another candidate list (security review 11-23, finding 4). The app refuses the same characters
	// (client/core/link/routeUpgrade.test.ts). U+0085 is one Go's TrimSpace would strip and JavaScript's trim keeps.
	for _, bad := range []string{"\n", "\r", "\x00", "\t", "\x7f", "\u0085"} {
		for _, candidate := range []PairingCandidate{
			{Name: "region-a" + bad + "wss://evil.test/ws", StableURL: "wss://quiet-otter.a.link.test/ws"},
			{Name: "region-a", StableURL: "wss://quiet-otter.a.link.test/ws" + bad},
			{Name: bad + "region-a", StableURL: "wss://quiet-otter.a.link.test/ws"},
		} {
			if payload, err := RouteBindingPayload("AA", "BB", "CC", "DD", 1700000000000, []PairingCandidate{candidate}); err == nil {
				t.Fatalf("binding with %q signed as %q", bad, payload)
			}
		}
	}
}
