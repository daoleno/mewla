package watcher

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// A delegated Claude prompt arrives as Mewla's typed prefix plus one paste
// envelope, so Claude's UserPromptSubmit (and its transcript) carries the
// wrapped bytes; the unwrapped digest is the submitted payload's. The poll
// that adopts provider evidence for a pending delegated prompt compared only
// the wrapped digest, so a Claude Worker that did not report was never
// admitted (admission.ambiguous, turn "unknown") although Claude answered: a
// fresh spawn and every `-command "claude --resume <id>"` continuation alike
// (owner 2026-11-13).
func TestClaudePendingAdmissionIsAdoptedByItsUnwrappedDigest(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{
		{Structured: true, FallbackAllowed: true},
		{Structured: true, FallbackAllowed: true},
	}}
	w := lifecycleTestWatcher(io, ledger, probe)
	w.targetCommandResolver = func(string) (string, bool) { return "claude", true }
	sessionID := "mh-claude-resume"
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Command: "claude --resume 68197d1b-d079-4cd2-9c44-c93a18189b6e", Cwd: "/repo/wt",
		PaneAlive: true, Delegated: true, State: classifier.StateUnknown,
	}
	turnID := sessionID + ":turn:1"
	payload := "continue the review"
	if result, err := w.SubmitDelegatedInput(sessionID, payload, turnID, now); err != nil || result.Outcome != InputAccepted {
		t.Fatalf("transport submit = (%+v, %v)", result, err)
	}
	pendingList, _ := ledger.PendingInputAdmissions(sessionID)
	if len(pendingList) != 1 {
		t.Fatalf("pending = %+v", pendingList)
	}
	pending := pendingList[0]
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if pending.PayloadSHA256 != digest {
		t.Fatalf("pending digest %s, payload digest %s", pending.PayloadSHA256, digest)
	}

	observed := ProviderActivityObservation{
		ID: "68197d1b:activity:u-9", Status: "running", StartedAt: now.Add(2 * time.Second),
		AdmissionStream: "events\x0068197d1b-d079-4cd2-9c44-c93a18189b6e", AdmissionID: "prompt-9", AdmissionCursor: 1,
		AdmissionAt:          now.Add(2 * time.Second),
		InputSHA256:          fmt.Sprintf("%x", sha256.Sum256([]byte("typed prefix + <pasted_content …>"))),
		InputUnwrappedSHA256: digest,
		Structured:           true, FallbackAllowed: true,
	}
	resolved, ok := w.resolvePendingProviderAdmission(pending, observed, now.Add(3*time.Second))
	if !ok || resolved.State != InputAdmissionResolved || resolved.ResolvedTurnID == "" {
		t.Fatalf("Claude's unwrapped admission did not adopt the pending prompt: ok=%v %+v", ok, resolved)
	}
	if resolved.ResolvedAdmission.SHA256 != digest {
		t.Fatalf("resolved admission records %q, want the submitted digest", resolved.ResolvedAdmission.SHA256)
	}

	// Bytes that are neither the payload nor its envelope still never claim it.
	other := observed
	other.InputSHA256, other.InputUnwrappedSHA256 = "aa", "bb"
	pendingList, _ = ledger.PendingInputAdmissions(sessionID)
	for _, p := range pendingList {
		if _, ok := w.resolvePendingProviderAdmission(p, other, now.Add(4*time.Second)); ok {
			t.Fatal("unrelated bytes adopted a pending prompt")
		}
	}
}
