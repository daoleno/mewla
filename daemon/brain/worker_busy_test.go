package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// running is the provider's record of a turn that has run since before this
// Mewla turn was accepted, under a name the turn never recorded (the transcript
// names it, the agent's events named it before a restart), at work just now.
func (f *idleWorkerFixture) running(input string) {
	f.fake.providerEvidence["worker"] = watcher.ProviderActivityObservation{
		ID: "transcript-activity", Status: "running", StartedAt: f.accepted.Add(-30 * time.Minute), ProgressAt: f.now,
		InputSHA256: pendingSubmissionDigest("Execute: " + input), InputUnwrappedSHA256: pendingSubmissionDigest(input),
	}
}

func (f *idleWorkerFixture) lostOrKept(t *testing.T, hours int, step func()) bool {
	t.Helper()
	for i := 0; i < hours*12; i++ {
		step()
		f.advance(5 * time.Minute)
		if st := f.state(t); st.Attempt == nil || st.Review != nil {
			return true
		}
	}
	return false
}

// ZeroGPrdUI, 2026-11-15 23:08 CST: Brain's prompt was queued into a Claude
// turn already running (no activity of its own starts for it), and the daemon
// restarted mid-turn, so the provider's record named the turn differently from
// the one Mewla kept. The Worker worked for another hour and was released as
// lost. A provider at work whose latest input is this turn's prompt keeps it.
func TestAQueuedTurnTheProviderWorksOnIsNeverLost(t *testing.T) {
	f := newIdleWorkerFixture(t, 300)
	if f.lostOrKept(t, 3, func() { f.running("task") }) {
		t.Fatalf("the Worker at work on its queued prompt was released: %+v", f.state(t))
	}
	if f.state(t).Attempt.LeaseEpoch != 0 {
		t.Fatal("its check-in lease ran out while the provider worked on its prompt")
	}
}

// The provider's record cannot be tied to the turn, but the Session's own
// screen shows Claude at work ("✻ Calculating… (1h 34m 36s · thinking with
// high effort)"): the turn is kept. When the screen goes quiet and nothing else
// shows work, the turn is lost as before, and the loss says what was seen.
func TestAWorkerSeenWorkingOnItsScreenIsNeverLostAndSilenceSaysWhy(t *testing.T) {
	f := newIdleWorkerFixture(t, 300)
	f.fake.working = true
	if f.lostOrKept(t, 3, func() { f.running("another prompt") }) {
		t.Fatalf("the Worker its screen shows at work was released: %+v", f.state(t))
	}
	f.fake.working = false
	if !f.lostOrKept(t, 2, func() { f.running("another prompt") }) {
		t.Fatal("a Worker with no sign of work was never released")
	}
	st := f.state(t)
	if st.Review == nil || st.Review.Reason != "turn_lost" {
		t.Fatalf("silence ended as %+v", st.Review)
	}
	events, err := f.store.ListWorkEvents(f.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var lost, evidence int
	for _, event := range events {
		switch event.Kind {
		case "turn_lost":
			lost++
			for _, want := range []string{"No sign of work since its check-in ran out", "its provider's record says running",
				"its screen does not show the agent working", "no background shell, agent or monitor"} {
				if !strings.Contains(event.Summary, want) {
					t.Errorf("the loss does not say %q: %q", want, event.Summary)
				}
			}
		case workEventKindStalled:
			evidence++
		}
	}
	if lost != 1 || evidence != 1 {
		t.Fatalf("loss events %d, evidence rows %d, want one each", lost, evidence)
	}
}
