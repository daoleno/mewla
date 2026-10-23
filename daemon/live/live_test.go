package live

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func worker(id, name, status, phase string, started time.Time) brain.WorkerRef {
	return brain.WorkerRef{ID: id, Name: name, Status: status, Phase: phase, Delegated: true, StartedAt: &started, Updated: started}
}

func TestProjectIdle(t *testing.T) {
	state := Project(brain.Snapshot{}, t0)
	if state.Kind != KindIdle || state.WorkerCount != 0 || state.Question != nil || state.RunKey != "" || state.StartedAt != 0 {
		t.Fatalf("idle state = %+v", state)
	}
}

func TestProjectRunningSingleUsesWorkTitleAndPhase(t *testing.T) {
	started := t0.Add(-20 * time.Second)
	snapshot := brain.Snapshot{
		Workers:     []brain.WorkerRef{worker("w1", "main:7", "running", "releasing", started)},
		CurrentWork: []brain.CurrentWork{{ID: "work-1", Title: "perpetuo-fre17-18", AttemptSessionID: "w1"}},
		Objective:   &brain.Objective{Title: "ship", Total: 5, Done: 2},
	}
	state := Project(snapshot, t0)
	if state.Kind != KindRunning || state.Title != "perpetuo-fre17-18" || state.Phase != "releasing" {
		t.Fatalf("state = %+v", state)
	}
	if state.Done != 2 || state.Total != 5 || state.WorkerCount != 1 || state.StartedAt != started.UnixMilli() {
		t.Fatalf("state = %+v", state)
	}
	if state.RunKey != "run:w1@"+itoa(started.UnixMilli()) {
		t.Fatalf("run key = %q", state.RunKey)
	}
}

func TestProjectRunningMultiCollapsesToCountAndIgnoresNonRunning(t *testing.T) {
	hidden := worker("h", "hidden", "running", "", t0)
	hidden.Hidden = true
	notDelegated := worker("n", "plain", "running", "", t0)
	notDelegated.Delegated = false
	snapshot := brain.Snapshot{Workers: []brain.WorkerRef{
		worker("a", "alpha", "running", "working", t0.Add(-3*time.Minute)),
		worker("b", "beta", "running", "reading", t0.Add(-1*time.Minute)),
		worker("c", "gamma", "blocked", "", t0.Add(-5*time.Minute)),
		worker("d", "delta", "running", "planning", t0.Add(-2*time.Minute)),
		worker("e", "eps", "running", "planning", t0.Add(-30*time.Second)),
		hidden, notDelegated,
	}}
	state := Project(snapshot, t0)
	if state.Title != "4 workers" || state.Phase != "" || state.WorkerCount != 4 || len(state.Workers) != 3 {
		t.Fatalf("state = %+v", state)
	}
	if state.StartedAt != t0.Add(-3*time.Minute).UnixMilli() {
		t.Fatalf("started_at must be the earliest start, got %d", state.StartedAt)
	}
	if !strings.HasPrefix(state.RunKey, "run:e@") {
		t.Fatalf("run key follows the newest start, got %q", state.RunKey)
	}
}

func TestProjectNeedsYouWorkQuestionOutranksRunningAndChat(t *testing.T) {
	since := t0.Add(-time.Minute)
	snapshot := brain.Snapshot{
		Workers: []brain.WorkerRef{worker("w1", "x", "running", "working", t0.Add(-time.Hour))},
		CurrentWork: []brain.CurrentWork{
			{ID: "old", Title: "Old", Question: "old?", Choices: []string{"a"}, AttentionSince: ptr(since.Add(-time.Hour))},
			{ID: "new", Title: "Ship v0.2.39", Question: "Ship v0.2.39 to everyone?", Choices: []string{"Ship", "Wait", "Cancel", "Fourth"}, AttentionSince: &since},
		},
		QuickReplies: &brain.QuickReplies{Rows: [][]string{{"Yes", "No"}}, At: t0},
	}
	state := Project(snapshot, t0)
	if state.Kind != KindNeedsYou || state.Question == nil || state.Question.Kind != QuestionWork || state.Question.WorkID != "new" {
		t.Fatalf("state = %+v", state)
	}
	if got := state.Question.Choices; len(got) != 3 || got[0] != "Ship" {
		t.Fatalf("choices = %v (max 3)", got)
	}
	if state.NeedsCount != 3 {
		t.Fatalf("needs_count = %d, want 2 Work questions + chat chips", state.NeedsCount)
	}
	if state.RunKey != "ask:"+state.Question.ID {
		t.Fatalf("run key = %q", state.RunKey)
	}
	if choice, ok := state.Question.FullChoice(1); !ok || choice != "Wait" {
		t.Fatalf("full choice = %q %v", choice, ok)
	}
}

func TestProjectNeedsYouChatChips(t *testing.T) {
	snapshot := brain.Snapshot{QuickReplies: &brain.QuickReplies{Rows: [][]string{{"Yes", "No"}, {"Later"}}, At: t0}}
	state := Project(snapshot, t0)
	if state.Kind != KindNeedsYou || state.Question.Kind != QuestionChat || state.Question.Text != "" {
		t.Fatalf("state = %+v", state)
	}
	if len(state.Question.Choices) != 3 || state.Question.Choices[2] != "Later" || state.NeedsCount != 1 {
		t.Fatalf("state = %+v", state)
	}
	// A different set of chips is a different question.
	other := Project(brain.Snapshot{QuickReplies: &brain.QuickReplies{Rows: [][]string{{"Yes", "No"}}, At: t0.Add(time.Second)}}, t0)
	if other.Question.ID == state.Question.ID {
		t.Fatal("question id must change with the chips")
	}
}

func TestProjectBoundsTheText(t *testing.T) {
	long := strings.Repeat("长", 300)
	snapshot := brain.Snapshot{CurrentWork: []brain.CurrentWork{{ID: "w", Title: "t", Question: long, Choices: []string{strings.Repeat("x", 90)}}}}
	state := Project(snapshot, t0)
	if n := len([]rune(state.Question.Text)); n > maxQuestionText {
		t.Fatalf("question text %d runes", n)
	}
	if n := len([]rune(state.Question.Choices[0])); n > maxChoiceRunes {
		t.Fatalf("choice %d runes", n)
	}
	if full, _ := state.Question.FullChoice(0); len(full) != 90 {
		t.Fatalf("full choice must stay whole, got %d", len(full))
	}
	data, _ := json.Marshal(state)
	if len(data) > 2048 {
		t.Fatalf("payload %d bytes", len(data))
	}
}

type sink struct {
	mu     sync.Mutex
	states []State
}

func (s *sink) add(state State) { s.mu.Lock(); s.states = append(s.states, state); s.mu.Unlock() }
func (s *sink) all() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]State(nil), s.states...)
}

func TestProjectorReportsOnlyMaterialChanges(t *testing.T) {
	out := &sink{}
	p := NewProjector(1000, 0, out.add)
	running := func(phase string, started time.Time) brain.Snapshot {
		return brain.Snapshot{Workers: []brain.WorkerRef{worker("w", "w", "running", phase, started)}}
	}
	p.Observe(brain.Snapshot{})
	p.Observe(brain.Snapshot{})
	p.Observe(running("working", t0))
	// Same run, only the clock anchor and the updated stamp move.
	again := running("working", t0)
	again.Workers[0].Updated = t0.Add(time.Minute)
	p.Observe(again)
	p.Observe(running("verifying", t0))
	p.Observe(brain.Snapshot{})

	states := out.all()
	kinds := []string{}
	for _, s := range states {
		kinds = append(kinds, s.Kind+":"+s.Phase)
	}
	want := []string{"idle:", "running:working", "running:verifying", "idle:"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("reports = %v, want %v", kinds, want)
	}
	for i, s := range states {
		if s.Epoch != 1000 || s.Seq != uint64(i+1) {
			t.Fatalf("state %d numbering = %d/%d", i, s.Epoch, s.Seq)
		}
	}
	if p.Current().Seq != 4 {
		t.Fatalf("current = %+v", p.Current())
	}
}

func TestProjectorSequenceOrdersAcrossRestarts(t *testing.T) {
	first := &sink{}
	a := NewProjector(1000, 0, first.add)
	a.Observe(brain.Snapshot{})
	a.Observe(brain.Snapshot{QuickReplies: &brain.QuickReplies{Rows: [][]string{{"Y"}}, At: t0}})
	second := &sink{}
	b := NewProjector(2000, 0, second.add)
	b.Observe(brain.Snapshot{})
	lastOld, firstNew := first.all()[1], second.all()[0]
	if firstNew.Seq >= lastOld.Seq {
		t.Fatal("test needs the restarted counter to be lower than the old one")
	}
	if !(firstNew.Epoch > lastOld.Epoch) {
		t.Fatal("a restart must start a later epoch so (epoch, seq) keeps ordering")
	}
}

func TestProjectorDebounceCoalescesBurst(t *testing.T) {
	out := &sink{}
	p := NewProjector(1, 40*time.Millisecond, out.add)
	for _, phase := range []string{"reading", "planning", "working"} {
		p.Observe(brain.Snapshot{Workers: []brain.WorkerRef{worker("w", "w", "running", phase, t0)}})
	}
	if len(out.all()) != 0 {
		t.Fatal("nothing is reported before the debounce")
	}
	time.Sleep(150 * time.Millisecond)
	states := out.all()
	if len(states) != 1 || states[0].Phase != "working" {
		t.Fatalf("burst must collapse to the newest state, got %+v", states)
	}
}

func ptr(t time.Time) *time.Time { return &t }
