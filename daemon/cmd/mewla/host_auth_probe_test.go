package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
)

func TestHostProbesArePassiveUntilExactAgentUse(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := map[string]int{}
	probe, auth, unsupported, account := newHostProbes(func(id string) doctor.ExecutorsCheck {
		calls[id]++
		switch id {
		case "codex":
			return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{{ID: id, BinaryFound: true, Runnable: true, Auth: doctor.AuthAuthenticated, VerifiedAuthenticated: true, Account: "person@example.com"}}}
		case "cursor-alias":
			return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{{ID: id, BinaryFound: true, Auth: doctor.AuthUnauthenticated}}}
		default:
			return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{{ID: id, BinaryFound: true, Summary: "unsupported"}}}
		}
	}, func() time.Time { return now })
	for _, id := range []string{"codex", "cursor-alias", "opencode"} {
		if auth(id) != brain.HostAuthUnknown || unsupported(id) != "" || account(id) != "" {
			t.Fatalf("%s had evidence before a probe", id)
		}
	}
	now = now.Add(2 * hostAuthSettledTTL)
	if len(calls) != 0 {
		t.Fatalf("passive reads started probes: %v", calls)
	}
	probe("codex")
	if auth("codex") != brain.HostAuthAuthenticated || account("codex") != "person@example.com" {
		t.Fatal("active Codex evidence missing")
	}
	now = now.Add(hostAuthProbeTTL + time.Second)
	probe("codex")
	if calls["codex"] != 1 || calls["cursor-alias"] != 0 {
		t.Fatalf("settled probe crossed agents: %v", calls)
	}
	now = now.Add(hostAuthSettledTTL)
	probe("codex")
	if calls["codex"] != 2 {
		t.Fatalf("settled expiry: %v", calls)
	}
	probe("cursor-alias")
	if auth("cursor-alias") != brain.HostAuthUnauthenticated {
		t.Fatal("signed-out distinction lost")
	}
	now = now.Add(hostAuthProbeTTL + time.Second)
	probe("cursor-alias")
	if calls["cursor-alias"] != 2 || calls["codex"] != 2 {
		t.Fatalf("short expiry crossed agents: %v", calls)
	}
	probe("opencode")
	if unsupported("opencode") != "unsupported" {
		t.Fatal("unsupported version reason lost")
	}
}
