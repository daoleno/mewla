package main

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
)

func TestHostAuthProbeAnswersAsDoctorDoesAndCaches(t *testing.T) {
	calls := 0
	now := time.Unix(1000, 0)
	probe := newHostAuthProbe(func() doctor.ExecutorsCheck {
		calls++
		return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{
			{ID: "claude", BinaryFound: true, Auth: doctor.AuthAuthenticated, VerifiedAuthenticated: true},
			{ID: "codex", BinaryFound: true, Auth: doctor.AuthUnauthenticated},
			{ID: "grok", BinaryFound: true, Auth: doctor.AuthUnknown},
		}}
	}, func() time.Time { return now })

	for id, want := range map[string]brain.HostAuthState{
		"claude": brain.HostAuthAuthenticated,
		"codex":  brain.HostAuthUnauthenticated,
		"grok":   brain.HostAuthUnknown,
		"pi":     brain.HostAuthUnknown,
	} {
		if got := probe(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if calls != 1 {
		t.Fatalf("doctor ran %d times within the TTL, want 1", calls)
	}
	now = now.Add(hostAuthProbeTTL + time.Second)
	probe("claude")
	if calls != 2 {
		t.Fatalf("doctor ran %d times after the TTL, want 2", calls)
	}
}

func TestHostProbesReportAnInstalledButUnsupportedAgent(t *testing.T) {
	calls := 0
	auth, unsupported := newHostProbes(func() doctor.ExecutorsCheck {
		calls++
		return doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{
			{ID: "opencode", BinaryFound: true, Auth: doctor.AuthUnknown, Runnable: false, Summary: "OpenCode 1.1.25 found; Mewla supports OpenCode 2 only."},
			{ID: "codex", BinaryFound: true, Auth: doctor.AuthUnauthenticated, Runnable: false, Summary: "codex found but not authenticated"},
			{ID: "grok", BinaryFound: true, Auth: doctor.AuthUnknown, Runnable: true},
			{ID: "pi", BinaryFound: false},
		}}
	}, func() time.Time { return time.Unix(1000, 0) })
	if got := unsupported("opencode"); got != "OpenCode 1.1.25 found; Mewla supports OpenCode 2 only." {
		t.Fatalf("opencode = %q", got)
	}
	for _, id := range []string{"codex", "grok", "pi"} {
		if got := unsupported(id); got != "" {
			t.Fatalf("%s is not unsupported, got %q", id, got)
		}
	}
	if auth("codex") != brain.HostAuthUnauthenticated {
		t.Fatal("signed-out codex lost")
	}
	if calls != 1 {
		t.Fatalf("doctor ran %d times, want 1 (shared cache)", calls)
	}
}
