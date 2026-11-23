package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

func noteCompactions(t *testing.T, store *Store, note string) []Work {
	t.Helper()
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	var out []Work
	for _, item := range items {
		if strings.HasPrefix(item.ID, noteCompactionPrefix+note+":") {
			out = append(out, item)
		}
	}
	return out
}

// A note over its budget is archived as it is and raises exactly one
// compaction Work: not again while it is open, not again within a day of the
// last, not again after a restart; the note itself is never touched.
func TestANoteOverItsBudgetRaisesOneCompactionWork(t *testing.T) {
	store, service, clock := waitEndFixture(t)
	memory := filepath.Join(store.WorkspacePath(), "memory.md")
	content := "# Brain Memory\n\n" + strings.Repeat("a fact that may be stale\n", 1500) // ~36 KB > 32 KiB
	if err := os.WriteFile(memory, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := service.raiseNoteCompactions(); err != nil {
			t.Fatal(err)
		}
	}
	works := noteCompactions(t, store, "memory.md")
	if len(works) != 1 {
		t.Fatalf("compaction Works = %d, want 1", len(works))
	}
	if !strings.Contains(works[0].Objective, store.NoteArchiveDir()) {
		t.Fatalf("the Work does not name the archive: %q", works[0].Objective)
	}
	events, _ := store.ListWorkEvents(works[0].ID)
	if len(events) != 1 || events[0].Kind != noteCompactionKind || !strings.HasPrefix(events[0].PayloadRef, store.NoteArchiveDir()) {
		t.Fatalf("events = %+v", events)
	}
	archives, _ := filepath.Glob(filepath.Join(store.NoteArchiveDir(), "*-memory.md"))
	if len(archives) != 1 {
		t.Fatalf("archives = %v", archives)
	}
	if raw, _ := os.ReadFile(archives[0]); string(raw) != content {
		t.Fatal("the archive is not the note as it was")
	}
	if raw, _ := os.ReadFile(memory); string(raw) != content {
		t.Fatal("the note itself was changed")
	}
	if st, _ := store.fsmState(works[0].ID); st == nil || st.Review == nil {
		t.Fatalf("no review opened for Brain: %+v", st)
	}
	for _, note := range []string{"current.md", "profile.md", routingGuideName} {
		if got := noteCompactions(t, store, note); len(got) != 0 {
			t.Fatalf("%s under budget raised %d", note, len(got))
		}
	}

	// A restart (a new Service on the same store) raises no second.
	again := newTestService(t, store, &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Worker{}}, nil)
	again.now = clock.at
	if err := again.raiseNoteCompactions(); err != nil {
		t.Fatal(err)
	}
	if got := noteCompactions(t, store, "memory.md"); len(got) != 1 {
		t.Fatalf("after a restart: %d Works", len(got))
	}

	// Closed but still over budget: not again within a day, then once.
	item, _ := store.Work(works[0].ID)
	if _, err := service.CloseWork(WorkCloseRequest{WorkID: item.ID, ExpectedRevision: item.Revision, Status: WorkCancelled,
		Actor: lifecycle.ActorRuntime, Reason: "test"}); err != nil && err != ErrWorkCleanupPending {
		t.Fatal(err)
	}
	clock.advance(time.Hour)
	_ = service.raiseNoteCompactions()
	if got := noteCompactions(t, store, "memory.md"); len(got) != 1 {
		t.Fatalf("within a day of the last: %d Works", len(got))
	}
	clock.advance(24 * time.Hour)
	_ = service.raiseNoteCompactions()
	if got := noteCompactions(t, store, "memory.md"); len(got) != 2 {
		t.Fatalf("a day later, still over budget: %d Works, want 2", len(got))
	}
}
