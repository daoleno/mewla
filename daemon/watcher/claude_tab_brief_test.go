package watcher

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A delegated brief holding a tab. Claude Code's prompt editor turns every
// tab into four spaces, so its transcript carries "Step 1:    read" and the
// provider admission digest is that text's. The turn the brief opened must
// still resolve from that evidence instead of staying ambiguous.
func TestClaudeTabBriefResolvesItsTurn(t *testing.T) {
	brief := "Fix the duplicate.\nStep 1:\tread the rows\nStep 2:\tclaim the echo"
	recorded := strings.ReplaceAll(brief, "\t", "    ")
	recordedDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(recorded)))

	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("claude")
	turn := testTurnDraft("tab-brief", time.Now().UTC(), identity)
	owner := newLedgerSessionInputOwner(io, ledger)
	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity), identity.Command,
		brief, turn, scriptedCorrelatedAdmission(recorded),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turn.ID {
		t.Fatalf("a tab brief must resolve its turn from Claude's record: (%+v, %v)", result, err)
	}
	if len(io.submissions) != 1 || io.submissions[0] != recorded {
		t.Fatalf("Mewla must type the bytes Claude records: %q", io.submissions)
	}
	if submission, found, _ := owner.inputAdmission("agent:@1", turn.ID); !found || submission.PayloadSHA256 != recordedDigest {
		t.Fatalf("the turn's payload digest must be the recorded text's %s: %+v found=%v", recordedDigest, submission, found)
	}
	// The relaunch reader compares a dead agent's conversation with the same
	// digest: the brief it holds is the brief Mewla sent.
	pending := InputAdmission{PayloadSHA256: recordedDigest}
	if got := deadPromptDelivery(pending, ProviderActivityObservation{ID: "m", InputSHA256: "wrapped", InputUnwrappedSHA256: recordedDigest, ProbeState: ProbeStateOK}); got != PromptHeld {
		t.Fatalf("relaunch must find the held tab brief, got %v", got)
	}
}

// Other providers record the submitted bytes, tab included.
func TestCodexBriefKeepsItsTab(t *testing.T) {
	brief := "Step 1:\tread the rows"
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("codex")
	turn := testTurnDraft("codex-tab-brief", time.Now().UTC(), identity)
	owner := newLedgerSessionInputOwner(io, newFakeTurnLedger())
	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity), identity.Command,
		brief, turn, scriptedCorrelatedAdmission(brief),
	)
	if err != nil || result.Outcome != InputAccepted || len(io.submissions) != 1 || io.submissions[0] != brief {
		t.Fatalf("codex brief = (%+v, %v), typed %q", result, err, io.submissions)
	}
}
