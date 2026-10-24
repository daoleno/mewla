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
	if proof.fresh("%1", identity, now, 0, false) {
		t.Fatal("nothing proven yet, yet fresh")
	}
	proof.record("%1", identity, now, 0)
	if !proof.fresh("%1", identity, now.Add(hostIdentityReproofInterval-time.Second), 0, false) {
		t.Fatal("proof expired early")
	}
	if proof.fresh("%1", identity, now.Add(hostIdentityReproofInterval), 0, false) {
		t.Fatal("proof outlived the interval")
	}
	if proof.fresh("%2", identity, now, 0, false) {
		t.Fatal("proof carried over to another Host")
	}
	changed := identity
	changed.SessionID = "s2"
	if proof.fresh("%1", changed, now, 0, false) {
		t.Fatal("proof carried over to a different saved identity")
	}
	if proof.fresh("%1", work.HostTranscriptIdentity{}, now, 0, false) {
		t.Fatal("an unbound identity counts as proven")
	}
}

// An agent that announces its conversation changes is asked about no sooner
// than it announces one; one that does not is asked on the timer.
func TestHostIdentityProofFollowsAnnouncementsWhenTheyCoverTheHost(t *testing.T) {
	var proof hostIdentityProof
	identity := work.HostTranscriptIdentity{Provider: "claude", SessionID: "s1", Path: "/s1.jsonl"}
	now := time.Now()
	proof.record("%1", identity, now, 4)
	late := now.Add(10 * hostIdentityReproofInterval)
	if !proof.fresh("%1", identity, late, 4, true) {
		t.Fatal("a covered Host was re-proved on the timer")
	}
	if proof.fresh("%1", identity, now, 5, true) {
		t.Fatal("a rebind did not invalidate the proof")
	}
	if proof.fresh("%1", identity, late, 4, false) {
		t.Fatal("an uncovered Host skipped the timed recheck")
	}
}
