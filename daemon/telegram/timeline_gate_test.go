package telegram

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// timelineBrain counts how often the Brain timeline is read.
type timelineBrain struct {
	*fakeBrain
	reads atomic.Int64
}

func (b *timelineBrain) ThreadTimeline(threadID string, limit int) ([]brain.TimelineItem, error) {
	b.reads.Add(1)
	return b.fakeBrain.ThreadTimeline(threadID, limit)
}

// versionedTimelineBrain also says when the timeline may have changed.
type versionedTimelineBrain struct {
	timelineBrain
	version atomic.Int64
}

func (b *versionedTimelineBrain) ConversationProjectionVersion() uint64 {
	return uint64(b.version.Load())
}

func (b *versionedTimelineBrain) append(item brain.TimelineItem) {
	b.fakeBrain.mu.Lock()
	b.fakeBrain.timeline = append(b.fakeBrain.timeline, item)
	b.fakeBrain.mu.Unlock()
	b.version.Add(1)
}

func gatedManager(t *testing.T, versioned bool) (*Manager, *timelineBrain, *versionedTimelineBrain) {
	t.Helper()
	manager, fake, _, _ := configuredManager(t)
	bindOwner(t, manager, 1, 10, 10)
	if versioned {
		owner := &versionedTimelineBrain{timelineBrain: timelineBrain{fakeBrain: fake}}
		manager.brain = owner
		return manager, &owner.timelineBrain, owner
	}
	owner := &timelineBrain{fakeBrain: fake}
	manager.brain = owner
	return manager, owner, nil
}

func assistantItem(manager *Manager, id, body string) brain.TimelineItem {
	return brain.TimelineItem{ID: id, ThreadID: "thread-1", Role: "assistant", Kind: "assistant_message", Body: body, CreatedAt: manager.now().Add(time.Second)}
}

func TestTimelineIsNotProjectedWhenNothingWasAppended(t *testing.T) {
	manager, reads, _ := gatedManager(t, true)
	for range 6 {
		if err := manager.projectTimelineIfChanged(); err != nil {
			t.Fatal(err)
		}
	}
	// The first pass reads the timeline; the pass after it sees the outbox the
	// first one filled and reads once more; then it is stable.
	if got := reads.reads.Load(); got > 2 {
		t.Fatalf("an idle timeline was read %d times in 6 passes", got)
	}
	settled := reads.reads.Load()
	for range 20 {
		if err := manager.projectTimelineIfChanged(); err != nil {
			t.Fatal(err)
		}
	}
	if got := reads.reads.Load(); got != settled {
		t.Fatalf("idle passes kept projecting: %d -> %d reads", settled, got)
	}
}

func TestAppendedTimelineItemIsProjectedOnTheNextPass(t *testing.T) {
	manager, reads, owner := gatedManager(t, true)
	for range 3 {
		if err := manager.projectTimelineIfChanged(); err != nil {
			t.Fatal(err)
		}
	}
	before := reads.reads.Load()
	owner.append(assistantItem(manager, "answer-1", "hello from Brain"))
	if err := manager.projectTimelineIfChanged(); err != nil {
		t.Fatal(err)
	}
	if reads.reads.Load() == before {
		t.Fatal("an appended timeline item did not wake the projection")
	}
	found := false
	for _, row := range manager.store.snapshot().Outbox {
		if row.CanonicalID == "answer-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the appended item was not enqueued: %+v", manager.store.snapshot().Outbox)
	}
}

func TestTimelineProjectionRepeatsAfterTheSafetyNet(t *testing.T) {
	manager, reads, _ := gatedManager(t, true)
	clock := manager.now()
	manager.now = func() time.Time { return clock }
	for range 3 {
		if err := manager.projectTimelineIfChanged(); err != nil {
			t.Fatal(err)
		}
	}
	before := reads.reads.Load()
	clock = clock.Add(timelineSafetyNet + time.Second)
	if err := manager.projectTimelineIfChanged(); err != nil {
		t.Fatal(err)
	}
	if reads.reads.Load() == before {
		t.Fatal("the safety-net projection never ran")
	}
}

func TestUnversionedBrainIsProjectedEveryPass(t *testing.T) {
	manager, reads, _ := gatedManager(t, false)
	for range 4 {
		if err := manager.projectTimelineIfChanged(); err != nil {
			t.Fatal(err)
		}
	}
	if got := reads.reads.Load(); got != 4 {
		t.Fatalf("reads = %d, want one per pass", got)
	}
}

func TestOutboxDrainingReopensAProjectionThatTheOutboxRefused(t *testing.T) {
	manager, reads, owner := gatedManager(t, true)
	for range 3 {
		if err := manager.projectTimelineIfChanged(); err != nil {
			t.Fatal(err)
		}
	}
	owner.append(assistantItem(manager, "answer-2", "queued"))
	if err := manager.projectTimelineIfChanged(); err != nil {
		t.Fatal(err)
	}
	if err := manager.projectTimelineIfChanged(); err != nil {
		t.Fatal(err)
	}
	settled := reads.reads.Load()
	if err := manager.deliverOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := manager.projectTimelineIfChanged(); err != nil {
		t.Fatal(err)
	}
	if reads.reads.Load() == settled {
		t.Fatal("a change to the outbox did not reopen the projection")
	}
}

// The production Brain must be versioned, or the gate silently never closes.
var _ versionedBrain = (*brain.Service)(nil)
