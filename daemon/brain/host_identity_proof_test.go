package brain

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

func TestHostIdentityProofStandsUntilTheIntervalPasses(t *testing.T) {
	var proof hostIdentityProof
	identity := work.HostTranscriptIdentity{Provider: "codex", SessionID: "s1", Path: "/rollout-s1.jsonl"}
	now := time.Now()
	if proof.fresh("%1", identity, now) {
		t.Fatal("nothing proven yet, yet fresh")
	}
	proof.record("%1", identity, now)
	if !proof.fresh("%1", identity, now.Add(hostIdentityReproofInterval-time.Second)) {
		t.Fatal("proof expired early")
	}
	if proof.fresh("%1", identity, now.Add(hostIdentityReproofInterval)) {
		t.Fatal("proof outlived the interval")
	}
	if proof.fresh("%2", identity, now) {
		t.Fatal("proof carried over to another Host")
	}
	changed := identity
	changed.SessionID = "s2"
	if proof.fresh("%1", changed, now) {
		t.Fatal("proof carried over to a different saved identity")
	}
	if proof.fresh("%1", work.HostTranscriptIdentity{}, now) {
		t.Fatal("an unbound identity counts as proven")
	}
}
