package work

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

type codexLookupSnapshot struct{ selections, subprocesses, headers, hits int64 }

func readCodexLookupCost() codexLookupSnapshot {
	return codexLookupSnapshot{
		codexLookupCost.Selections.Load(), codexLookupCost.Subprocesses.Load(),
		codexLookupCost.HeaderParses.Load(), codexLookupCost.SelectionHits.Load(),
	}
}

func (a codexLookupSnapshot) since(b codexLookupSnapshot) codexLookupSnapshot {
	return codexLookupSnapshot{a.selections - b.selections, a.subprocesses - b.subprocesses, a.headers - b.headers, a.hits - b.hits}
}

type codexIdleFixture struct {
	sqlite3, dbPath, rollout, cwd string
	worker                        classifier.Worker
}

func newCodexIdleFixture(t *testing.T, now time.Time) codexIdleFixture {
	t.Helper()
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(home, ".codex", "sessions", "2026", "10", "23", "rollout-2026-10-23T10-00-00-thread-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, rollout,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-1", "cwd": cwd, "originator": "codex-tui"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "hello"}},
	)
	if err := os.Chtimes(rollout, now, now); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	runSQLite(t, sqlite3, dbPath, `CREATE TABLE threads (id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, cwd TEXT, archived INTEGER, created_at_ms INTEGER, updated_at_ms INTEGER);
INSERT INTO threads VALUES ('thread-1', `+sqlString(rollout)+`, `+itoa64(now.Unix())+`, `+itoa64(now.Unix())+`, `+sqlString(cwd)+`, 0, `+itoa64(now.UnixMilli())+`, `+itoa64(now.UnixMilli())+`);`)
	return codexIdleFixture{
		sqlite3: sqlite3, dbPath: dbPath, rollout: rollout, cwd: cwd,
		worker: classifier.Worker{ID: "brain:@1", Name: "Brain", Cwd: cwd, Command: "codex", ProcessID: os.Getpid(), StartedAt: now.Add(-time.Minute), Hidden: true},
	}
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// An idle Codex pane is polled twice a second. Each poll used to fork sqlite3,
// ps and lsof and parse rollout headers; now it costs a few stat calls.
func TestIdleCodexPollsReuseTheSelectionInsteadOfSpawning(t *testing.T) {
	now := time.Now()
	f := newCodexIdleFixture(t, now)
	const polls = 20

	// What every poll cost before: one full selection each.
	before := readCodexLookupCost()
	for i := 0; i < polls; i++ {
		if _, ok, err := findCodexTranscript(f.worker, now); err != nil || !ok {
			t.Fatalf("baseline lookup: ok=%v err=%v", ok, err)
		}
	}
	uncached := readCodexLookupCost().since(before)
	if uncached.selections != polls || uncached.subprocesses < 2*polls {
		t.Fatalf("baseline %+v: expected %d selections and at least %d spawns", uncached, polls, 2*polls)
	}

	reader := NewProviderConversationReader()
	before = readCodexLookupCost()
	for i := 0; i < polls; i++ {
		conversation, err := reader.Load(f.worker, WorkerProviderCodex, now.Add(time.Duration(i)*500*time.Millisecond))
		if err != nil || !conversation.Available || conversation.Path != f.rollout {
			t.Fatalf("poll %d: %+v %v", i, conversation, err)
		}
	}
	idle := readCodexLookupCost().since(before)
	if idle.selections != 1 || idle.hits != polls-1 {
		t.Fatalf("idle polls %+v: want one selection and %d reuses", idle, polls-1)
	}
	if one := uncached.subprocesses / polls; idle.subprocesses > one || idle.headers != 0 {
		t.Fatalf("idle polls spawned %d (one selection spawns %d) and parsed %d headers", idle.subprocesses, one, idle.headers)
	}
	t.Logf("%d idle polls: %d spawns (was %d), %d header parses", polls, idle.subprocesses, uncached.subprocesses, idle.headers)
}

func TestCodexSelectionIsRevisitedWhenStateChangesAgesOrVanishes(t *testing.T) {
	now := time.Now()
	f := newCodexIdleFixture(t, now)
	reader := NewProviderConversationReader()
	load := func(at time.Time) (CodexConversation, codexLookupSnapshot) {
		before := readCodexLookupCost()
		conversation, err := reader.Load(f.worker, WorkerProviderCodex, at)
		if err != nil {
			t.Fatal(err)
		}
		return conversation, readCodexLookupCost().since(before)
	}
	if _, cost := load(now); cost.selections != 1 {
		t.Fatalf("first load %+v", cost)
	}
	if _, cost := load(now.Add(time.Second)); cost.selections != 0 || cost.hits != 1 {
		t.Fatalf("second load %+v", cost)
	}

	// Codex records a new thread: the database changes, so the choice is remade.
	later := now.Add(2 * time.Second)
	runSQLite(t, f.sqlite3, f.dbPath, `INSERT INTO threads VALUES ('thread-2', '/nonexistent/rollout.jsonl', 1, 1, `+sqlString(f.cwd)+`, 0, 1, 1);`)
	if _, cost := load(later); cost.selections != 1 {
		t.Fatalf("a database change was not noticed: %+v", cost)
	}

	// Nothing changed, but the answer is older than the bound.
	if _, cost := load(later.Add(codexSelectionMaxAge + time.Second)); cost.selections != 1 {
		t.Fatalf("an old selection was kept: %+v", cost)
	}

	// The chosen rollout disappears: no stale path is returned.
	if err := os.Remove(f.rollout); err != nil {
		t.Fatal(err)
	}
	conversation, cost := load(later.Add(codexSelectionMaxAge + 2*time.Second))
	if cost.selections != 1 || conversation.Available {
		t.Fatalf("a vanished rollout was still served: %+v available=%v", cost, conversation.Available)
	}
}

func TestLsofIsAskedOnceForTheWholeProcessTree(t *testing.T) {
	before := codexLookupCost.Subprocesses.Load()
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	lsofOpenCodexRolloutPaths([]int{os.Getpid(), os.Getppid(), 1})
	if spawned := codexLookupCost.Subprocesses.Load() - before; spawned != 1 {
		t.Fatalf("lsof spawned %d times for three processes, want 1", spawned)
	}
	before = codexLookupCost.Subprocesses.Load()
	if paths := lsofOpenCodexRolloutPaths(nil); paths != nil || codexLookupCost.Subprocesses.Load() != before {
		t.Fatal("no processes must spawn nothing")
	}
}

func TestABoundWorkersRolloutIsUsedWithoutAnyLookup(t *testing.T) {
	now := time.Now()
	fx := newCodexIdleFixture(t, now)
	previous := CodexBoundRollout
	defer func() { CodexBoundRollout = previous }()
	other := filepath.Join(filepath.Dir(fx.rollout), "rollout-2026-10-23T10-05-00-thread-2.jsonl")
	writeJSONL(t, other, map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-2", "cwd": fx.cwd, "originator": "codex-tui"}})
	CodexBoundRollout = func(id string) string {
		if id == fx.worker.ID {
			return other
		}
		return ""
	}
	before := readCodexLookupCost()
	reader := NewProviderConversationReader()
	candidate, ok, err := reader.selectCodexTranscript(fx.worker, now)
	if err != nil || !ok || candidate.Path != other || candidate.Meta.ID != "thread-2" {
		t.Fatalf("candidate = %+v, %v, %v (the database's thread is %s)", candidate, ok, err, fx.rollout)
	}
	if cost := readCodexLookupCost().since(before); cost.selections != 0 || cost.subprocesses != 0 {
		t.Fatalf("a bound Worker ran the discovery: %+v", cost)
	}

	// Bound, but the rollout is not written yet: no turn, and no guessing at
	// another thread in the same folder.
	CodexBoundRollout = func(string) string { return filepath.Join(filepath.Dir(fx.rollout), "not-yet.jsonl") }
	if _, ok, err := reader.selectCodexTranscript(fx.worker, now); ok || err != nil {
		t.Fatalf("an unwritten rollout was found: %v, %v", ok, err)
	}
	if cost := readCodexLookupCost().since(before); cost.selections != 0 {
		t.Fatalf("an unwritten rollout fell back to discovery: %+v", cost)
	}

	// A Worker with no binding is found the old way.
	CodexBoundRollout = func(string) string { return "" }
	if candidate, ok, _ := reader.selectCodexTranscript(fx.worker, now); !ok || candidate.Path != fx.rollout {
		t.Fatalf("unbound Worker candidate = %+v, %v", candidate, ok)
	}
}
