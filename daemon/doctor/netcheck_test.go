package doctor

import (
	"context"
	"testing"
	"time"
)

func TestNetcheckText(t *testing.T) {
	const output = `2026/10/05 portmap: monitor: gateway changed
Report:
 * UDP: true
 * MappingVariesByDestIP: false
 * Nearest DERP: Los Angeles
 * DERP latency:
   - lax: 192.1ms (Los Angeles)
   - sea: 196.6ms (Seattle)
`
	e := env{opts: Options{ProbeTimeout: 3 * time.Second, LookPath: func(string) (string, error) { return "tailscale", nil }, RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) != 1 || args[0] != "netcheck" {
			t.Fatalf("args=%v", args)
		}
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) < 10*time.Second {
			t.Fatal("netcheck needs its own longer deadline")
		}
		return []byte(output), nil
	}}}
	got := e.checkNetwork()
	if got.Status != StatusOK || !got.Direct || got.Relay != "lax" || got.NATType != "cone/endpoint-independent" {
		t.Fatalf("%+v", got)
	}
	udp, mapping, _, ok := parseNetcheckText("* UDP: false\n* MappingVariesByDestIP: true")
	if !ok || udp || mapping != "true" {
		t.Fatal("blocked/symmetric report not parsed")
	}
	if _, _, _, ok := parseNetcheckText("diagnostics only"); ok {
		t.Fatal("must not invent report fields")
	}
}
