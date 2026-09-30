package watcher

import (
	"strings"
	"testing"
	"time"
)

func TestClaudeDelegatedBriefHasTypedIntentOutsidePaste(t *testing.T) {
	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	owner.ledger = newFakeTurnLedger()
	identity := testSessionInputIdentity("claude")
	_, err := owner.submitDelegated("agent:@1", identity, fixedSessionInputResolver(identity), "claude", "assigned brief", delegatedTurnDraft{WorkID: "work", ID: "turn:brief", AcceptedAt: time.Now().UTC(), ProcessIdentity: delegatedTurnIdentity(identity), SignalProtocol: true}, scriptedCorrelatedAdmission("assigned brief"))
	if err != nil {
		t.Fatal(err)
	}
	if len(io.queues) != 1 {
		t.Fatal("brief must use one target-bound queue")
	}
	queue := strings.Join(io.queues[0], "|")
	if !strings.Contains(queue, "send-keys|-l|-t|"+io.paneValue.paneID+"|Execute: ") {
		t.Fatalf("delegated brief is only untrusted pasted content: %s", queue)
	}
}
