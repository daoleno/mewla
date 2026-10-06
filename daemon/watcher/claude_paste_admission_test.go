package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestClaudePastePendingReconcilesWithoutReplay(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	ledger.resolveErr = errors.New("resolve persistence failed")
	identity := testSessionInputIdentity("claude")
	payload := "payload"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	turn := testTurnDraft("resolve-persist-failure", time.Now().UTC(), identity)
	owner := newLedgerSessionInputOwner(io, ledger)
	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity), identity.Command,
		payload, turn, scriptedCorrelatedAdmission(payload),
	)
	if InputOutcomeFromError(err) != InputAmbiguous || result.Outcome != InputAmbiguous || len(io.queues) != 1 {
		t.Fatalf("resolve persistence failure = (%+v, %v), queues=%d", result, err, len(io.queues))
	}
	if _, found, _ := ledger.Turn("agent:@1"); found {
		t.Fatal("failed resolve exposed a fresh canonical Turn")
	}
	ledger.resolveErr = nil
	reconcile := delegatedInputConfirmer{baseline: func() (delegatedInputBaseline, error) {
		return delegatedInputBaseline{
			Admission: delegatedAdmissionEvidence{
				Stream: "test", ID: "after", Cursor: 2,
				StartedAt: turn.AcceptedAt.Add(time.Second), InputSHA256: "raw-wrapper-digest", InputUnwrappedSHA256: digest,
			},
			Provider: ProviderActivityObservation{
				ID: "activity-accepted", Status: "running", Structured: true,
			},
		}, nil
	}}
	restarted := newLedgerSessionInputOwner(io, ledger)
	result, err = restarted.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity), identity.Command,
		payload, turn, reconcile,
	)
	if err != nil || result.Outcome != InputAccepted || !result.Duplicate || result.TurnID != turn.ID {
		t.Fatalf("restart reconciliation = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("restart reconciliation replayed input: queues=%d", len(io.queues))
	}
}

func TestClaudePasteConfirmationPreservesExactDigestAndFences(t *testing.T) {
	at := time.Now().UTC()
	observation := ProviderActivityObservation{ID: "activity", Status: "running", AdmissionStream: "stream", AdmissionID: "new", AdmissionCursor: 2, AdmissionAt: at, InputSHA256: "raw", InputUnwrappedSHA256: "inner"}
	w := watcherWithAdmissionProbe(&fixedStateProbe{obs: observation})
	confirmation, err := w.delegatedInputConfirmer("agent:@1", "claude").confirm(delegatedAdmissionEvidence{Stream: "stream", ID: "old", Cursor: 1}, at, "inner")
	if err != nil || confirmation.Outcome != InputAccepted || confirmation.Admission.InputSHA256 != "inner" {
		t.Fatalf("confirmation=%+v err=%v", confirmation, err)
	}
	current := delegatedAdmissionEvidenceFromObservation(observation)
	for _, baseline := range []delegatedAdmissionEvidence{{Stream: "other", Cursor: 1}, {Stream: "stream", Cursor: 2}, {Stream: "stream", ID: "new", Cursor: 1}} {
		if correlateDelegatedAdmission(baseline, current, at, "inner") != delegatedAdmissionMissing {
			t.Fatal("wrapper bypassed identity fence")
		}
	}
	if correlateDelegatedAdmission(delegatedAdmissionEvidence{}, current, at.Add(time.Second), "inner") != delegatedAdmissionMissing {
		t.Fatal("wrapper bypassed timestamp fence")
	}
	if correlateDelegatedAdmission(delegatedAdmissionEvidence{}, current, at, "different") != delegatedAdmissionMismatched {
		t.Fatal("different payload matched")
	}
}
