package watch

import (
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// strictBrain keeps Brain's rule for a receipt: it names one input for good,
// and the same receipt with another text is refused ("Brain input admission
// identity belongs to different input"). A refused-once receipt reports
// notSubmitted for its first `refuse` tries, then takes the input.
type strictBrain struct {
	bodies  map[string]string
	taken   map[string]bool
	refuse  int
	tries   map[string]int
	history []string
}

func newStrictBrain() *strictBrain {
	return &strictBrain{bodies: map[string]string{}, taken: map[string]bool{}, tries: map[string]int{}}
}

func (b *strictBrain) SubmitExternalUserInput(receipt, body string) (brain.ExternalInputDisposition, error) {
	if known, ok := b.bodies[receipt]; ok && known != body {
		return brain.ExternalInputNotSubmitted, fmt.Errorf("Brain input admission identity belongs to different input")
	}
	b.bodies[receipt] = body
	b.tries[receipt]++
	if b.taken[receipt] {
		return brain.ExternalInputAccepted, nil
	}
	if b.tries[receipt] <= b.refuse {
		return brain.ExternalInputNotSubmitted, nil
	}
	b.taken[receipt] = true
	b.history = append(b.history, receipt)
	return brain.ExternalInputAccepted, nil
}

// The brief of a later morning about the very same problems (after the
// cooldown) is a new input. It used to reuse the first brief's receipt with a
// new date in its text, which Brain refuses for good: the calendar retried it
// every minute until the problems changed or the occurrence was skipped.
func TestABriefAboutTheSameProblemsOnALaterDayIsANewInput(t *testing.T) {
	f := newNotifyFixture(t, time.Date(2026, 10, 22, 0, 30, 0, 0, time.UTC)) // 08:30 Shanghai
	strict := newStrictBrain()
	f.n.brain = strict
	f.problem("a", "Terminal scroll still janky", 60)

	first := f.clock
	if err := f.n.briefDue(first); err != nil {
		t.Fatal(err)
	}
	f.advance(briefCooldown + time.Hour)
	f.advance(-time.Hour) // 08:30 again, the cooldown just over
	second := f.clock
	if err := f.n.briefDue(second); err != nil {
		t.Fatalf("the later brief was refused: %v", err)
	}
	if len(strict.history) != 2 || strict.history[0] == strict.history[1] {
		t.Fatalf("receipts taken = %v, want two different ones", strict.history)
	}
}

// A brief Brain did not take is retried as it was: the same receipt and the
// same text, even a minute later and after a restart, and it is delivered once.
func TestARetriedBriefKeepsItsReceiptAndText(t *testing.T) {
	f := newNotifyFixture(t, time.Date(2026, 10, 22, 0, 30, 0, 0, time.UTC))
	strict := newStrictBrain()
	strict.refuse = 2
	f.n.brain = strict
	f.problem("a", "Terminal scroll still janky", 60)
	due := f.clock

	if err := f.n.briefDue(due); err == nil {
		t.Fatal("a brief Brain did not take reported success")
	}
	f.advance(time.Minute)
	reopened, err := newNotifier(f.store, strict, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.briefDue(due); err == nil {
		t.Fatal("a brief Brain did not take reported success")
	}
	f.advance(time.Minute)
	if err := reopened.briefDue(due); err != nil {
		t.Fatalf("the retry was refused: %v", err)
	}
	if len(strict.bodies) != 1 || len(strict.history) != 1 {
		t.Fatalf("receipts seen=%d taken=%d, want one input retried", len(strict.bodies), len(strict.history))
	}
	if err := reopened.briefDue(due); err != nil || len(strict.history) != 1 {
		t.Fatalf("an answered occurrence was sent again: %v %v", err, strict.history)
	}
}
