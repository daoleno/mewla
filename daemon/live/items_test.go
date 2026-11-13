package live

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

func TestItemsWorkRowCarriesItsSessionAgentAndRing(t *testing.T) {
	joined := t0.Add(-12 * time.Minute)
	in := Input{
		Snapshot: brain.Snapshot{CurrentWork: []brain.CurrentWork{{
			ID: "w1", Title: "Quickstart rewrite", Status: brain.WorkRunning, AttemptSessionID: "s1",
			Progress:       &brain.WorkProgress{Stage: brain.ProgressMoving, Percent: 43},
			WorkerSessions: []brain.WorkWorkerSession{{SessionID: "s1", Provider: "claude", Live: true, JoinedAt: joined}},
		}}},
		Sessions: []Session{{ID: "s1", Title: "main:4", Provider: "claude", Line: "Editing docs/quickstart.md", Delegated: true, Running: true, StartedAt: joined}},
	}
	state := ProjectInput(in, t0)
	if state.Kind != KindRunning || state.Running != 1 || len(state.Items) != 1 {
		t.Fatalf("a Work and its own Session are one running row: %+v", state)
	}
	item := state.Items[0]
	if item.Key != "work:w1" || item.Open != OpenSession || item.ID != "s1" || item.Agent != "claude" || item.Title != "Quickstart rewrite" || item.Line != "Editing docs/quickstart.md" {
		t.Fatalf("item = %+v", item)
	}
	if item.Ring == nil || item.Ring.Stage != brain.ProgressMoving || item.Ring.Percent != 45 {
		t.Fatalf("ring rounds to 5: %+v", item.Ring)
	}
	if item.StartedAt != joined.UnixMilli() || item.Needs != "" {
		t.Fatalf("item = %+v", item)
	}
}

func TestItemsRingRules(t *testing.T) {
	cases := []struct {
		name string
		work brain.CurrentWork
		want *Ring
	}{
		{"capped at 95", brain.CurrentWork{Status: brain.WorkRunning, Progress: &brain.WorkProgress{Stage: brain.ProgressMoving, Percent: 99}}, &Ring{Stage: brain.ProgressMoving, Percent: 95}},
		{"back", brain.CurrentWork{Status: brain.WorkRunning, Progress: &brain.WorkProgress{Stage: brain.ProgressBack, Percent: 61}}, &Ring{Stage: brain.ProgressBack, Percent: 60}},
		{"last stretch", brain.CurrentWork{Status: brain.WorkWaiting, Progress: &brain.WorkProgress{Stage: brain.ProgressLast, Percent: 99}}, &Ring{Stage: brain.ProgressLast}},
		{"unmeasured", brain.CurrentWork{Status: brain.WorkRunning, Progress: &brain.WorkProgress{Stage: brain.ProgressUnmeasured}}, nil},
		{"not judged", brain.CurrentWork{Status: brain.WorkRunning}, nil},
		{"a question hides it", brain.CurrentWork{Status: brain.WorkRunning, Question: "go?", Progress: &brain.WorkProgress{Stage: brain.ProgressMoving, Percent: 40}}, nil},
		{"parked on a named wait", brain.CurrentWork{Status: brain.WorkWaiting, WaitFor: "the owner's key", Progress: &brain.WorkProgress{Stage: brain.ProgressMoving, Percent: 40}}, nil},
		{"closed", brain.CurrentWork{Status: brain.WorkDone, Progress: &brain.WorkProgress{Stage: brain.ProgressLast}}, nil},
	}
	for _, tc := range cases {
		got := ringOf(tc.work)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Errorf("%s: ring = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestItemsNeedsFirstAndCountsAsTheHeader(t *testing.T) {
	since := t0.Add(-time.Minute)
	in := Input{
		Snapshot: brain.Snapshot{
			CurrentWork: []brain.CurrentWork{
				{ID: "q", Title: "Pricing page", Status: brain.WorkRunning, Question: "14-day or\n 7-day trial?", Choices: []string{"14-day", "7-day"}, AttentionSince: &since,
					Progress: &brain.WorkProgress{Stage: brain.ProgressMoving, Percent: 50}},
				{ID: "r", Title: "Release notes", Status: brain.WorkRunning, WorkerSessions: []brain.WorkWorkerSession{
					{SessionID: "a", Provider: "codex", Live: true, JoinedAt: t0.Add(-30 * time.Minute)},
					{SessionID: "b", Provider: "claude", Live: true, JoinedAt: t0.Add(-20 * time.Minute)},
				}},
				{ID: "wait", Title: "Waits", Status: brain.WorkWaiting},
			},
			OpenWork: []brain.OpenWork{{ID: "later", Title: "Later", Status: brain.WorkOpen}},
		},
		Sessions: []Session{
			{ID: "a", Delegated: true, Running: true},
			{ID: "b", Delegated: true, Running: true},
			{ID: "mine", Title: "mewla-site", Provider: "opencode", Line: "Run bun run build", Running: true, StartedAt: t0.Add(-4 * time.Minute)},
			{ID: "ask", Title: "infra", Provider: "codex", Asks: NeedsApprove, Ask: "Run git push origin main", Requests: 2, AskSince: t0.Add(-2 * time.Minute)},
		},
	}
	state := ProjectInput(in, t0)
	if state.Kind != KindNeedsYou || state.NeedsCount != 3 || state.Running != 2 || state.Waiting != 1 || state.Queued != 1 {
		t.Fatalf("counts: kind=%s needs=%d running=%d waiting=%d queued=%d", state.Kind, state.NeedsCount, state.Running, state.Waiting, state.Queued)
	}
	if len(state.Items) != 3 {
		t.Fatalf("at most three items, got %d", len(state.Items))
	}
	first, second, third := state.Items[0], state.Items[1], state.Items[2]
	if first.Key != "work:q" || first.Needs != NeedsAnswer || first.Line != "14-day or 7-day trial?" || first.Ring != nil {
		t.Fatalf("the newest request first, its ask as the line, no ring: %+v", first)
	}
	if second.Key != "session:ask" || second.Needs != NeedsApprove || second.Agent != "codex" || second.Open != OpenSession {
		t.Fatalf("second = %+v", second)
	}
	if third.Key != "session:mine" {
		t.Fatalf("then the running row that started last: %+v", third)
	}
	if state.Question == nil || state.RunKey != "ask:"+state.Question.ID {
		t.Fatalf("the Work question keeps its answer path: %+v", state.Question)
	}
	data, _ := json.Marshal(state)
	if len(data) > 3000 {
		t.Fatalf("payload %d bytes: too big for a push", len(data))
	}
}

func TestItemsSeveralWorkersAreOneRowWithBrainsMark(t *testing.T) {
	in := Input{Snapshot: brain.Snapshot{CurrentWork: []brain.CurrentWork{{ID: "r", Title: "Release", Status: brain.WorkRunning, WorkerSessions: []brain.WorkWorkerSession{
		{SessionID: "a", Provider: "codex", Live: true, Summary: "Writing notes", JoinedAt: t0.Add(-3 * time.Minute)},
		{SessionID: "b", Provider: "claude", Live: true, JoinedAt: t0.Add(-time.Minute)},
	}}}}}
	state := ProjectInput(in, t0)
	if state.Running != 1 || len(state.Items) != 1 {
		t.Fatalf("state = %+v", state)
	}
	item := state.Items[0]
	if item.Agent != AgentBrain || item.Open != OpenWork || item.ID != "r" || item.Line != "Writing notes" {
		t.Fatalf("item = %+v", item)
	}
}

func TestItemsSessionThatAsksHidesItsWorkRow(t *testing.T) {
	in := Input{
		Snapshot: brain.Snapshot{CurrentWork: []brain.CurrentWork{{ID: "w", Title: "Deploy", Status: brain.WorkRunning, AttemptSessionID: "s",
			WorkerSessions: []brain.WorkWorkerSession{{SessionID: "s", Provider: "claude", Live: true}}}}},
		Sessions: []Session{{ID: "s", Title: "main:2", Provider: "claude", Delegated: true, Running: true, Asks: NeedsAnswer, Ask: "Sign in to npm"}},
	}
	state := ProjectInput(in, t0)
	if state.Running != 0 || state.NeedsCount != 1 || len(state.Items) != 1 || state.Items[0].Key != "session:s" {
		t.Fatalf("state = %+v", state)
	}
	if state.Kind != KindNeedsYou || state.Question != nil || state.RunKey == "" {
		t.Fatalf("a Session's request is needs you without a Work question: %+v", state)
	}
}

func TestItemsBrainsOwnPrompt(t *testing.T) {
	state := ProjectInput(Input{HostAsk: "Allow Bash: rm -rf build?", HostAskSince: t0}, t0)
	if state.Kind != KindNeedsYou || state.NeedsCount != 1 || len(state.Items) != 1 {
		t.Fatalf("state = %+v", state)
	}
	if item := state.Items[0]; item.Open != OpenBrain || item.Agent != AgentBrain || item.Needs != NeedsAnswer {
		t.Fatalf("item = %+v", item)
	}
}

func TestItemsQuietIsIdleWithCounts(t *testing.T) {
	in := Input{Snapshot: brain.Snapshot{OpenWork: []brain.OpenWork{
		{ID: "a", Status: brain.WorkOpen},
		{ID: "b", Status: brain.WorkWaiting},
	}}}
	state := ProjectInput(in, t0)
	if state.Kind != KindIdle || state.Queued != 1 || state.Waiting != 1 || len(state.Items) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

func TestItemClockIsNotANewState(t *testing.T) {
	var got []State
	p := NewProjector(1, 0, func(state State) { got = append(got, state) })
	in := func(started time.Time) Input {
		return Input{Sessions: []Session{
			{ID: "new", Title: "x", Running: true, StartedAt: t0.Add(time.Hour)},
			{ID: "s", Title: "y", Running: true, StartedAt: started},
		}}
	}
	p.Observe(in(t0))
	p.Observe(in(t0.Add(time.Second)))
	if len(got) != 1 {
		t.Fatalf("a moved clock anchor must not report again, got %d states", len(got))
	}
	if p.Current().Items[1].StartedAt != t0.Add(time.Second).UnixMilli() {
		t.Fatalf("but the current state keeps the newest anchor")
	}
}
