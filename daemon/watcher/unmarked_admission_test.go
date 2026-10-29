package watcher

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type unmarkedTestLedger struct {
	*fakeTurnLedger
	repaired int
}

func (l *unmarkedTestLedger) AbortUnmarkedInputAdmission(in InputAdmission) error {
	l.repaired++
	_, err := l.AbortInputAdmission(in.SessionID, in.ProposedTurnID, in.Receipt, in.PayloadSHA256)
	return err
}

func TestUnmarkedPreparationTransportGuards(t *testing.T) {
	for _, mode := range []string{"unmarked", "accepted-marker", "ambiguous-marker", "full-history", "different-generation", "other-work"} {
		t.Run(mode, func(t *testing.T) {
			io := newFakeSessionInputIO()
			ledger := &unmarkedTestLedger{fakeTurnLedger: newFakeTurnLedger()}
			identity := testSessionInputIdentity("codex")
			prior := InputAdmission{WorkID: "work", SessionID: "agent:@1", ProposedTurnID: "old", Receipt: "old", PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: io.paneValue.generation, SignalProtocol: true, Mode: InputAdmissionFresh, AcceptedAt: time.Now().UTC()}
			if mode == "different-generation" {
				prior.ProcessIdentity = "other"
			}
			if mode == "other-work" {
				prior.WorkID = "other"
			}
			ledger.PrepareInputAdmission(prior)
			if mode == "accepted-marker" || mode == "ambiguous-marker" {
				outcome := InputAccepted
				if mode == "ambiguous-marker" {
					outcome = InputAmbiguous
				}
				io.ledger.Entries = append(io.ledger.Entries, sessionInputReceiptEntry{Receipt: "old", PayloadSHA256: prior.PayloadSHA256, Outcome: outcome})
			}
			if mode == "full-history" {
				for i := 0; i < sessionInputReceiptLedgerLimit; i++ {
					io.ledger.Entries = append(io.ledger.Entries, sessionInputReceiptEntry{Receipt: fmt.Sprintf("receipt-%d", i), PayloadSHA256: prior.PayloadSHA256, Outcome: InputAccepted})
				}
			}
			owner := newSessionInputOwner(io)
			owner.ledger = ledger
			_, err := owner.submitDelegated(prior.SessionID, identity, fixedSessionInputResolver(identity), identity.Command, "explicit new input", delegatedTurnDraft{WorkID: "work", ID: "new", AcceptedAt: time.Now().UTC(), ProcessIdentity: delegatedTurnIdentity(identity), SignalProtocol: true}, scriptedCorrelatedAdmission("explicit new input"))
			if mode == "unmarked" {
				if err != nil || ledger.repaired != 1 {
					t.Fatalf("repair=%d err=%v", ledger.repaired, err)
				}
			} else if ledger.repaired != 0 {
				t.Fatalf("unsafe repair for %s", mode)
			}
			if mode == "full-history" || mode == "different-generation" {
				if err == nil || len(io.queues) != 0 {
					t.Fatal("unsafe condition crossed mutation boundary")
				}
			}
		})
	}
}

type staleAdmissionTestLedger struct {
	*fakeTurnLedger
	ambiguous []string
}

func (l *staleAdmissionTestLedger) MarkInputAdmissionAmbiguous(sessionID, proposedTurnID, reason string) error {
	l.ambiguous = append(l.ambiguous, proposedTurnID)
	return nil
}

// A delivered prompt whose progress signal has not come never fences a later
// send: not after the provider idled past it, and not while the provider is
// still busy, when the new prompt queues behind it like typed input.
func TestDeliveredPreparationNeverRefusesALaterSend(t *testing.T) {
	for _, mode := range []string{"idle-after", "running", "just-settled"} {
		t.Run(mode, func(t *testing.T) {
			io := newFakeSessionInputIO()
			ledger := &staleAdmissionTestLedger{fakeTurnLedger: newFakeTurnLedger()}
			identity := testSessionInputIdentity("claude")
			now := time.Now().UTC()
			prior := InputAdmission{WorkID: "work", SessionID: "agent:@1", ProposedTurnID: "old", Receipt: "old", PayloadSHA256: strings.Repeat("a", 64), ProcessIdentity: delegatedTurnIdentity(identity), PaneGeneration: io.paneValue.generation, SignalProtocol: true, Mode: InputAdmissionConditionalSteer, AcceptedAt: now.Add(-10 * time.Minute)}
			ledger.PrepareInputAdmission(prior)
			io.ledger.Entries = append(io.ledger.Entries, sessionInputReceiptEntry{Receipt: "old", PayloadSHA256: prior.PayloadSHA256, Outcome: InputAccepted})
			provider := ProviderActivityObservation{ID: "activity", Status: "completed", StartedAt: now.Add(-9 * time.Minute), SettledAt: now.Add(-2 * time.Minute), ProbeState: ProbeStateOK}
			switch mode {
			case "running":
				provider.Status, provider.SettledAt = "running", time.Time{}
			case "just-settled":
				provider.SettledAt = now.Add(-5 * time.Second)
			}
			confirm := scriptedCorrelatedAdmission("continue")
			base := confirm.baseline
			confirm.baseline = func() (delegatedInputBaseline, error) {
				baseline, err := base()
				baseline.Provider = provider
				return baseline, err
			}
			owner := newSessionInputOwner(io)
			owner.ledger = ledger
			result, err := owner.submitDelegated(prior.SessionID, identity, fixedSessionInputResolver(identity), identity.Command, "continue", delegatedTurnDraft{WorkID: "work", ID: "new", AcceptedAt: now, ProcessIdentity: delegatedTurnIdentity(identity), SignalProtocol: true}, confirm)
			if len(ledger.ambiguous) != 1 || ledger.ambiguous[0] != "old" {
				t.Fatalf("%s: the delivered preparation still fences the Work: %v (err=%v)", mode, ledger.ambiguous, err)
			}
			if err != nil || result.Outcome != InputAccepted {
				t.Fatalf("%s: the send was refused: outcome=%v err=%v", mode, result.Outcome, err)
			}
		})
	}
}
