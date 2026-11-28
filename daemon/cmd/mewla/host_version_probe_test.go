package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/doctor"
)

func TestHostProbesArePassiveUntilExactAgentUse(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := map[string]int{}
	probe, unsupported := newHostProbes(func(id string) doctor.ExecutorsCheck {
		calls[id]++
		switch id {
		case "codex":
			return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{{ID: id, BinaryFound: true, Runnable: true}}}
		default:
			return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{{ID: id, BinaryFound: true, Summary: "unsupported"}}}
		}
	}, func() time.Time { return now })
	for _, id := range []string{"codex", "opencode"} {
		if unsupported(id) != "" {
			t.Fatalf("%s had evidence before a probe", id)
		}
	}
	now = now.Add(2 * hostVersionSettledTTL)
	if len(calls) != 0 {
		t.Fatalf("passive reads started probes: %v", calls)
	}
	probe("codex")
	if unsupported("codex") != "" {
		t.Fatal("a runnable Codex reads as unsupported")
	}
	now = now.Add(hostVersionProbeTTL + time.Second)
	probe("codex")
	if calls["codex"] != 1 || calls["opencode"] != 0 {
		t.Fatalf("settled probe crossed agents: %v", calls)
	}
	now = now.Add(hostVersionSettledTTL)
	probe("codex")
	if calls["codex"] != 2 {
		t.Fatalf("settled expiry: %v", calls)
	}
	probe("opencode")
	if unsupported("opencode") != "unsupported" {
		t.Fatal("unsupported version reason lost")
	}
	now = now.Add(hostVersionProbeTTL + time.Second)
	probe("opencode")
	if calls["opencode"] != 2 {
		t.Fatalf("an unsupported agent was not rechecked for recovery: %v", calls)
	}
}
