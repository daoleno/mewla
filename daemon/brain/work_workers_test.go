package brain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A Work run by three Workers one after another (each spawned with -work
// while Brain handles the previous result) lists all three, in the order they
// joined, with each one's own outcome; the names and executors outlive the
// panes and a Store reopen.
func TestWorkListsEveryWorkerSessionInJoinOrder(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "host:@workers"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	host := &classifier.Worker{ID: hostID, Hidden: true, State: classifier.StateDone}
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{hostID: host}, ownedGenerations: map[string]string{hostID: "host-generation"}, outcomes: map[string]watcher.InputOutcome{}, turnStore: store}
	service := NewService(store, fw, nil)
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	service.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{Title: "ship the release", Objective: "release it", CompletionPolicy: CompletionUntilDone, DoneCriteriaRef: "Brain accepts the release"})
	if err != nil {
		t.Fatal(err)
	}
	signal := func(sessionID, token, kind, summary string) {
		t.Helper()
		now = now.Add(time.Minute)
		result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: sessionID, TurnID: token, Class: watcher.EvidenceControl, Kind: kind, SourceID: token + ":" + kind, Summary: summary, At: now, LeaseSeconds: 300})
		if err != nil || !result.Matched || !result.Changed {
			t.Fatalf("signal %s/%s: %+v %v", token, kind, result, err)
		}
	}
	join := func(sessionID, token, name, executor, purpose, purposeID string) {
		t.Helper()
		now = now.Add(time.Minute)
		if _, err := store.RecordWorkWorker(WorkWorkerRecord{WorkID: item.ID, SessionID: sessionID, Name: name, ExecutorID: executor, Provider: executor, JoinedAt: now}); err != nil {
			t.Fatal(err)
		}
		result, err := fw.SubmitDelegatedWorkInput(sessionID, "scoped "+token, item.ID, token, purpose, purposeID, now)
		if err != nil || result.Outcome != watcher.InputAccepted {
			t.Fatalf("submit %s: %+v %v", token, result, err)
		}
	}
	handOff := func(sessionID, token, name, executor string) {
		t.Helper()
		if woke, err := service.ReconcileHostLane(); err != nil || !woke {
			t.Fatalf("review admission: %v %v", woke, err)
		}
		lease := requireReviewDelivered(t, store, item.ID)
		join(sessionID, token, name, executor, string(lifecycle.AdmissionPurposeReview), lease.HandlingID)
		settleCanonicalHostTurnForTest(t, store, hostID, lease.ProviderTurnID)
	}

	join("worker:@a", "turn-a", "Planner", "claude", "", "")
	signal("worker:@a", "turn-a", "running", "Reading the release notes")
	signal("worker:@a", "turn-a", "done", "Release plan written")
	handOff("worker:@b", "turn-b", "Builder", "codex")
	signal("worker:@b", "turn-b", "running", "Building")
	signal("worker:@b", "turn-b", "failed", "Build broke on Windows")
	handOff("worker:@c", "turn-c", "Fixer", "claude")
	signal("worker:@c", "turn-c", "running", "Fixing the Windows build")

	// A and B are gone; C is open and running. The live Session's own state
	// wins over the ledger while it works on this Work.
	live := []WorkerRef{{ID: "worker:@c", Name: "Fixer (renamed)", Status: "running", Summary: "Rerunning the Windows build", Phase: "verifying", ExecutorID: "claude", Provider: "claude", Updated: now, Delegated: true}}
	check := func(store *Store) []WorkWorkerSession {
		t.Helper()
		inventory, err := store.ProjectWorkInventory(live)
		if err != nil {
			t.Fatal(err)
		}
		for _, current := range inventory.Current {
			if current.ID == item.ID {
				return current.WorkerSessions
			}
		}
		for _, open := range inventory.Open {
			if open.ID == item.ID {
				return open.WorkerSessions
			}
		}
		t.Fatalf("Work %s not projected: %+v", item.ID, inventory)
		return nil
	}
	for _, s := range []*Store{store, reopenStore(t, root)} {
		got := check(s)
		if len(got) != 3 {
			raw, _ := json.MarshalIndent(got, "", "  ")
			t.Fatalf("want 3 Workers, got %s", raw)
		}
		want := []struct {
			id, name, executor string
			state              WorkWorkerState
			summary            string
			live, ended        bool
		}{
			{"worker:@a", "Planner", "claude", WorkWorkerDone, "Release plan written", false, true},
			{"worker:@b", "Builder", "codex", WorkWorkerFailed, "Build broke on Windows", false, true},
			{"worker:@c", "Fixer (renamed)", "claude", WorkWorkerRunning, "Rerunning the Windows build", true, false},
		}
		for i, w := range want {
			row := got[i]
			if row.SessionID != w.id || row.Name != w.name || row.ExecutorID != w.executor || row.State != w.state ||
				row.Summary != w.summary || row.Live != w.live || (row.EndedAt != nil) != w.ended || row.JoinedAt.IsZero() {
				raw, _ := json.MarshalIndent(row, "", "  ")
				t.Fatalf("Worker %d = %s, want %+v", i, raw, w)
			}
			if i > 0 && !got[i-1].JoinedAt.Before(row.JoinedAt) {
				t.Fatalf("join order: %v then %v", got[i-1].JoinedAt, row.JoinedAt)
			}
		}
		if got[2].Phase != "verifying" {
			t.Fatalf("live phase = %q", got[2].Phase)
		}
	}

	detail, err := store.WorkDetail(item.ID, live)
	if err != nil || detail.ID != item.ID || len(detail.WorkerSessions) != 3 || detail.WorkerSessions[2].Name != "Fixer (renamed)" {
		t.Fatalf("detail = %+v %v", detail, err)
	}
	// The brief, whole, is on the detail only: never on the lists every phone gets with each snapshot.
	if brief, _ := store.Work(item.ID); brief.Objective == "" || detail.Objective != brief.Objective {
		t.Fatalf("detail objective = %q, Work's = %q", detail.Objective, brief.Objective)
	}
	inventory, _ := store.ProjectWorkInventory(live)
	if listed, _ := json.Marshal(inventory); strings.Contains(string(listed), `"objective"`) {
		t.Fatalf("a list carries the brief: %s", listed)
	}
	if _, err := store.WorkDetail("work-missing", live); err != ErrWorkNotFound {
		t.Fatalf("missing Work detail: %v", err)
	}
	raw, _ := json.MarshalIndent(detail, "", "  ")
	t.Logf("brain_work_detail.work = %s", raw)
	if got := (workWorkerIndex{}).workWorkerSessions("work-nobody-joined", nil); got != nil {
		t.Fatalf("a Work nobody joined lists %+v", got)
	}
}

// A Session that joined a Work without a record (spawned before records
// existed, or never seen by the reconcile tick) is still listed, unnamed.
func TestWorkWorkerWithoutRecordIsListedUnnamed(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	settled := now.Add(time.Hour)
	database := presentationDatabase{BrainTurns: []TurnRecord{
		{SessionID: "worker:@x", TurnID: "t1", WorkID: "work-1", Status: watcher.TurnDone, AcceptedAt: now, SettledAt: &settled, Summary: "Done", UpdatedAt: settled},
		{SessionID: "host:@h", TurnID: "h1", WorkID: "work-1", HostHandling: true, AcceptedAt: now, UpdatedAt: now},
	}}
	index := newWorkWorkerIndex(database, map[string]WorkWorkerRecord{}, nil, "host:@h")
	got := index.workWorkerSessions("work-1", nil)
	if len(got) != 1 || got[0].SessionID != "worker:@x" || got[0].Name != "" || got[0].State != WorkWorkerDone ||
		got[0].EndedAt == nil || !got[0].EndedAt.Equal(settled) {
		t.Fatalf("got %+v", got)
	}
}

// A Session whose newest turn belongs to another Work shows its outcome here,
// not its live state there; one that left with its turn still open ended.
func TestReusedWorkWorkerShowsThisWorksOutcome(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	settled := now.Add(time.Minute)
	database := presentationDatabase{BrainTurns: []TurnRecord{
		{SessionID: "worker:@r", TurnID: "t1", WorkID: "work-1", Status: watcher.TurnDone, AcceptedAt: now, SettledAt: &settled, Summary: "First done", UpdatedAt: settled},
		{SessionID: "worker:@r", TurnID: "t2", WorkID: "work-2", Status: watcher.TurnRunning, AcceptedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute)},
		{SessionID: "worker:@gone", TurnID: "t3", WorkID: "work-1", Status: watcher.TurnRunning, AcceptedAt: now.Add(3 * time.Minute), UpdatedAt: now.Add(4 * time.Minute)},
	}}
	live := []WorkerRef{{ID: "worker:@r", Name: "Reused", Status: "running", Summary: "On work-2", Delegated: true, Updated: now}}
	index := newWorkWorkerIndex(database, map[string]WorkWorkerRecord{}, live, "")
	got := index.workWorkerSessions("work-1", nil)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].SessionID != "worker:@r" || got[0].Live || got[0].State != WorkWorkerDone || got[0].Summary != "First done" {
		t.Fatalf("reused = %+v", got[0])
	}
	if got[1].SessionID != "worker:@gone" || got[1].Live || got[1].State != WorkWorkerEnded || got[1].EndedAt == nil {
		t.Fatalf("gone = %+v", got[1])
	}
	there := index.workWorkerSessions("work-2", nil)
	if len(there) != 1 || !there[0].Live || there[0].State != WorkWorkerRunning || there[0].Summary != "On work-2" {
		t.Fatalf("work-2 = %+v", there)
	}
}

func reopenStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
