package brain

import (
	"testing"
)

// Brain asks the person on a Work while its Worker's Attempt is still active
// (11-19 23:48: MewlaSocialDrafts' Worker was blocked on the owner's phone,
// and `work update -status waiting -question` failed with "Work already has an
// active Attempt"). The question reaches the card; the Attempt stays the
// Worker's; nothing is parked under it.
func TestBrainCanAskOnAWorkWhoseWorkerStillHoldsIt(t *testing.T) {
	f := newProgressBindingFixture(t)
	f.send(t, "turn:1")
	// The Worker runs, then stops on something only the person can do.
	for _, kind := range []string{"running", "attention"} {
		if result, err := f.report("turn:1", kind); err != nil || !result.Matched {
			t.Fatalf("report %s: %+v %v", kind, result, err)
		}
	}
	before, err := f.store.Work(f.workID)
	if err != nil {
		t.Fatal(err)
	}
	if before.AttemptSessionID == "" || f.attempt(t) == "" {
		t.Fatal("fixture has no active Attempt")
	}
	waiting := WorkWaiting
	question := "手机上打开无线调试了吗？"
	choices := []string{"已打开", "稍后"}
	recommended := "已打开"
	item, err := f.store.UpdateWork(f.workID, WorkUpdate{Status: &waiting, Question: &question, Choices: &choices, Recommended: &recommended})
	if err != nil {
		t.Fatalf("Brain could not ask while the Worker holds the Work: %v", err)
	}
	if item.Question != question || len(item.Choices) != 2 || item.Recommended != recommended {
		t.Fatalf("the card does not carry the question: %#v", item)
	}
	if item.AttemptSessionID != before.AttemptSessionID {
		t.Fatalf("the Attempt moved: %q, was %q", item.AttemptSessionID, before.AttemptSessionID)
	}
	if st, err := f.store.fsmState(f.workID); err != nil || st.Attempt == nil || st.Wake != nil {
		t.Fatalf("lifecycle after the question: %+v (%v)", st, err)
	}
	// Parking it with nothing asked still needs the Worker to be done with it.
	if _, err := f.store.UpdateWork(f.workID, WorkUpdate{Status: &waiting}); err == nil {
		t.Fatal("a Work its Worker holds was parked with nothing asked")
	}
}
