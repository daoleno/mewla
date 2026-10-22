package watch

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

func linkFixture(t *testing.T) (*Store, time.Time) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
}

func addJudged(t *testing.T, store *Store, id, title string, lastSeen time.Time, evidence ...string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.problems[id] = &Problem{
		ID: id, Rule: RuleJudgment, Subject: id, Title: title, Status: StatusNew, Active: true,
		Evidence: evidence, Count: len(evidence), FirstSeen: lastSeen.Add(-time.Hour), LastSeen: lastSeen,
	}
}

func TestApplyLinksMovesAProblemWithItsWork(t *testing.T) {
	store, seen := linkFixture(t)
	addJudged(t, store, "j_one", "Reply buttons dead", seen)
	work := brain.Work{ID: "work-fix-1", Status: brain.WorkRunning, ContextRef: WatchContextRef("j_one"), UpdatedAt: seen.Add(time.Hour)}

	if _, err := store.ApplyLinks([]brain.Work{work}, nil); err != nil {
		t.Fatal(err)
	}
	got := store.Problem("j_one")
	if got.Status != StatusFixing || !got.Active || len(got.Works) != 1 || got.Works[0].By != LinkContext {
		t.Fatalf("running Work: %#v", got)
	}

	work.Status, work.UpdatedAt = brain.WorkDone, seen.Add(2*time.Hour)
	if _, err := store.ApplyLinks([]brain.Work{work}, nil); err != nil {
		t.Fatal(err)
	}
	if got = store.Problem("j_one"); got.Status != StatusFixed || got.Active || got.FixedAt.IsZero() {
		t.Fatalf("accepted Work: %#v", got)
	}

	// New evidence after the fix reopens it.
	store.mu.Lock()
	store.problems["j_one"].LastSeen = seen.Add(3 * time.Hour)
	store.mu.Unlock()
	if _, err := store.ApplyLinks([]brain.Work{work}, nil); err != nil {
		t.Fatal(err)
	}
	if got = store.Problem("j_one"); got.Status != StatusRecurred || !got.Active {
		t.Fatalf("recurrence: %#v", got)
	}
}

func TestWorkThatClosedBeforeTheLastEvidenceIsNotTheFix(t *testing.T) {
	store, seen := linkFixture(t)
	addJudged(t, store, "j_two", "Upload fails", seen)
	if _, err := store.LinkWork("j_two", "work-old", LinkBrain); err != nil {
		t.Fatal(err)
	}
	old := brain.Work{ID: "work-old", Status: brain.WorkDone, UpdatedAt: seen.Add(-time.Hour)}
	if _, err := store.ApplyLinks([]brain.Work{old}, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.Problem("j_two"); got.Status != StatusNew {
		t.Fatalf("stale Work fixed it: %#v", got)
	}
}

func TestCancelledWorkReturnsAFixingProblemToNew(t *testing.T) {
	store, seen := linkFixture(t)
	addJudged(t, store, "j_three", "Slow start", seen)
	if _, err := store.LinkWork("j_three", "work-x", LinkBrain); err != nil {
		t.Fatal(err)
	}
	work := brain.Work{ID: "work-x", Status: brain.WorkOpen, UpdatedAt: seen.Add(time.Hour)}
	store.ApplyLinks([]brain.Work{work}, nil)
	if got := store.Problem("j_three"); got.Status != StatusFixing {
		t.Fatalf("open fix Work: %#v", got)
	}
	work.Status = brain.WorkCancelled
	store.ApplyLinks([]brain.Work{work}, nil)
	if got := store.Problem("j_three"); got.Status != StatusNew || !got.Active {
		t.Fatalf("cancelled Work: %#v", got)
	}
}

func TestOriginWorkNeedingInputIsNotHandling(t *testing.T) {
	store, seen := linkFixture(t)
	store.mu.Lock()
	store.problems["stall"] = &Problem{ID: "stall", Rule: RuleWorkerStalls, Subject: "work-stuck", Status: StatusNew, Active: true, LastSeen: seen}
	store.mu.Unlock()
	work := brain.Work{ID: "work-stuck", Status: brain.WorkNeedsInput, UpdatedAt: seen}
	store.ApplyLinks([]brain.Work{work}, nil)
	got := store.Problem("stall")
	if got.Status != StatusNew || len(got.Works) != 1 || got.Works[0].By != LinkSubject {
		t.Fatalf("stalled Work waiting on the user: %#v", got)
	}
	work.Status = brain.WorkRunning
	store.ApplyLinks([]brain.Work{work}, nil)
	if got = store.Problem("stall"); got.Status != StatusFixing {
		t.Fatalf("running again: %#v", got)
	}
}

func TestEvidenceAndMentionLinkWorkAndDismissalStands(t *testing.T) {
	store, seen := linkFixture(t)
	signal := Signal{ID: "p_sig", Source: SourceProcess, Kind: "turn_lost", Time: seen, Refs: map[string]string{"work": "work-evidence-1"}}
	if _, err := store.Append([]Signal{signal}); err != nil {
		t.Fatal(err)
	}
	addJudged(t, store, "j_four", "Retry loops, see work-mentioned-2", seen, "p_sig")
	addJudged(t, store, "j_five", "Noise", seen)
	if _, err := store.LabelProblem("j_five", LabelNot, "flaky", LabelByBrain); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LinkWork("j_five", "work-evidence-1", LinkBrain); err != nil {
		t.Fatal(err)
	}
	works := []brain.Work{
		{ID: "work-evidence-1", Status: brain.WorkRunning, UpdatedAt: seen},
		{ID: "work-mentioned-2", Status: brain.WorkDone, UpdatedAt: seen.Add(time.Minute)},
	}
	if _, err := store.ApplyLinks(works, nil); err != nil {
		t.Fatal(err)
	}
	got := store.Problem("j_four")
	by := map[string]string{}
	for _, link := range got.Works {
		by[link.WorkID] = link.By
	}
	if by["work-evidence-1"] != LinkEvidence || by["work-mentioned-2"] != LinkMention {
		t.Fatalf("links = %#v", got.Works)
	}
	if got.Status != StatusFixing {
		t.Fatalf("a running Work and an accepted one: %#v", got)
	}
	if dismissed := store.Problem("j_five"); dismissed.Status != StatusDismissed || dismissed.Label.By != LabelByBrain {
		t.Fatalf("dismissal overridden: %#v", dismissed)
	}
}

func TestLabelByDefaultsToTheUser(t *testing.T) {
	store, seen := linkFixture(t)
	addJudged(t, store, "j_six", "X", seen)
	got, err := store.LabelProblem("j_six", LabelLater, "", "")
	if err != nil || got.Label.By != LabelByUser || got.Status != StatusNew {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestAcceptedWorkTheProblemHappenedInDoesNotFixIt(t *testing.T) {
	store, seen := linkFixture(t)
	signal := Signal{ID: "p_sig2", Source: SourceProcess, Kind: "turn_lost", Time: seen, Refs: map[string]string{"work": "work-origin"}}
	if _, err := store.Append([]Signal{signal}); err != nil {
		t.Fatal(err)
	}
	addJudged(t, store, "j_origin", "Review loop never advances", seen, "p_sig2")
	done := brain.Work{ID: "work-origin", Status: brain.WorkDone, UpdatedAt: seen.Add(time.Hour)}
	if _, err := store.ApplyLinks([]brain.Work{done}, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.Problem("j_origin"); got.Status != StatusNew || len(got.Works) != 1 {
		t.Fatalf("origin Work fixed it: %#v", got)
	}
	// Work opened for the problem does.
	fix := brain.Work{ID: "work-fix", Status: brain.WorkDone, Objective: "Fix j_origin", UpdatedAt: seen.Add(2 * time.Hour)}
	if _, err := store.ApplyLinks([]brain.Work{done, fix}, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.Problem("j_origin"); got.Status != StatusFixed {
		t.Fatalf("fix Work: %#v", got)
	}
}

func TestAcceptanceTimeComesFromTheWorkNotItsUpdatedAt(t *testing.T) {
	store, seen := linkFixture(t)
	addJudged(t, store, "j_time", "Upload fails", seen)
	if _, err := store.LinkWork("j_time", "work-t", LinkBrain); err != nil {
		t.Fatal(err)
	}
	// UpdatedAt is stamped by every projection; the Work really closed before the problem.
	work := brain.Work{ID: "work-t", Status: brain.WorkDone, UpdatedAt: seen.Add(48 * time.Hour)}
	closed := func(brain.Work) time.Time { return seen.Add(-time.Hour) }
	if _, err := store.ApplyLinks([]brain.Work{work}, closed); err != nil {
		t.Fatal(err)
	}
	if got := store.Problem("j_time"); got.Status != StatusNew {
		t.Fatalf("restamped Work fixed it: %#v", got)
	}
}

func TestFixedByAnEarlierRuleIsTakenBackWhenTheWorkNoLongerSupportsIt(t *testing.T) {
	store, seen := linkFixture(t)
	addJudged(t, store, "j_old", "Loop", seen)
	store.mu.Lock()
	p := store.problems["j_old"]
	p.Status, p.Active, p.FixedAt, p.FixedReason = StatusFixed, false, seen.Add(time.Hour), linkFixedReason
	p.Works = []WorkLink{{WorkID: "work-origin", By: LinkEvidence}}
	store.mu.Unlock()
	work := brain.Work{ID: "work-origin", Status: brain.WorkDone, UpdatedAt: seen.Add(time.Hour)}
	if _, err := store.ApplyLinks([]brain.Work{work}, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.Problem("j_old"); got.Status != StatusNew || !got.Active || !got.FixedAt.IsZero() {
		t.Fatalf("%#v", got)
	}
}
