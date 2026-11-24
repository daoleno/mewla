package server

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

func TestEndedSessionRunStillResolvesItsFilesAfterARestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	other := t.TempDir()
	writeFiles(t, cwd, map[string]string{"reports/q3.html": "<p>q3</p>", "notes.md": "# n"})
	writeFiles(t, other, map[string]string{"elsewhere.md": "# e"})
	f := newSessionPageFixture(t, cwd)
	started := f.worker.StartedAt

	// While it runs, its row names its live run, and the run is recorded.
	row := f.server.workerSessionWire(f.worker)
	if row.FileRun == nil || row.FileRun.ProcessID != f.worker.ProcessID || row.FileRun.StartedAt != started.UnixMilli() {
		t.Fatalf("live file_run = %#v", row.FileRun)
	}

	// The agent exits (the row keeps the Session, without a process) and the daemon restarts.
	restarted := New(f.manager, nil, nil, nil, nil, nil, nil)
	done := &classifier.Worker{ID: f.worker.ID, Cwd: cwd, State: classifier.StateDone}
	restarted.sessionFileWorkerLoader = func(id string) *classifier.Worker {
		if id != done.ID {
			return nil
		}
		copy := *done
		return &copy
	}
	row = restarted.workerSessionWire(done)
	if row.FileRun == nil || row.FileRun.ProcessID != f.worker.ProcessID || row.FileRun.StartedAt != started.UnixMilli() {
		t.Fatalf("ended file_run = %#v", row.FileRun)
	}
	ended := clientMessage{WorkerID: f.worker.ID, ProcessID: f.worker.ProcessID, StartedAt: []byte(strconv.FormatInt(started.UnixMilli(), 10))}

	// Its references resolve as they did live: relative ones against its folder, absolute ones anywhere.
	for _, reference := range []string{"reports/q3.html", "./notes.md", filepath.Join(other, "elsewhere.md")} {
		ended.Path = reference
		resolved, err := restarted.resolveCurrentSessionFile(ended)
		if err != nil {
			t.Fatalf("%s: %v", reference, err)
		}
		resolved.file.Close()
	}
	if got := existingSessionFiles(cwd, []string{"reports/q3.html", "missing.md"}); !reflect.DeepEqual(got, []string{"reports/q3.html"}) {
		t.Fatalf("existing = %q", got)
	}

	// A run never seen live is refused as before.
	never := ended
	never.ProcessID++
	never.Path = "notes.md"
	if _, err := restarted.resolveCurrentSessionFile(never); !isStaleSessionFileIdentity(err) {
		t.Fatalf("a run never seen live: %v", err)
	}

	// The folder is collected (a worktree, scratch): relative references are gone, said as such; absolute ones still open.
	if err := os.RemoveAll(cwd); err != nil {
		t.Fatal(err)
	}
	ended.Path = "reports/q3.html"
	if _, err := restarted.resolveCurrentSessionFile(ended); err != errSessionFileMissing {
		t.Fatalf("a file of a collected folder: %v", err)
	}
	ended.Path = filepath.Join(other, "elsewhere.md")
	resolved, err := restarted.resolveCurrentSessionFile(ended)
	if err != nil {
		t.Fatalf("an absolute file after the folder went: %v", err)
	}
	if resolved.relativePath != filepath.ToSlash(resolved.canonicalPath) {
		t.Fatalf("shown path = %q", resolved.relativePath)
	}
	resolved.file.Close()
}

func TestSessionRunsAreBoundedAndKeepTheNewest(t *testing.T) {
	runs := &sessionRuns{path: filepath.Join(t.TempDir(), "session-runs.json")}
	start := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	for i := 0; i < maxSessionRuns+20; i++ {
		runs.note(&classifier.Worker{ID: "w" + strconv.Itoa(i), ProcessID: 100 + i, StartedAt: start.Add(time.Duration(i) * time.Second), Cwd: "/work"})
	}
	reloaded := &sessionRuns{path: runs.path}
	if _, ok := reloaded.last("w0"); ok {
		t.Fatal("the oldest run was kept past the bound")
	}
	if run, ok := reloaded.last("w" + strconv.Itoa(maxSessionRuns+19)); !ok || run.Cwd != "/work" {
		t.Fatalf("the newest run: %#v %v", run, ok)
	}
	// A Worker without a live run records nothing.
	runs.note(&classifier.Worker{ID: "idle", Cwd: "/work"})
	if _, ok := runs.last("idle"); ok {
		t.Fatal("a Worker with no process was recorded")
	}
}

// An ended Worker keeps no command; its chat binds by its last run's launch command (the conversation it
// names), never by whichever conversation is newest in a folder other Sessions share. A Worker running
// something now, or one never recorded, is left as it is.
func TestEndedRunWorkerTakesItsLastRunsCommand(t *testing.T) {
	srv := &Server{sessionRuns: &sessionRuns{loaded: true}}
	started := time.UnixMilli(1_700_000_000_000)
	srv.sessionRuns.note(&classifier.Worker{ID: "w-1", ProcessID: 41, StartedAt: started, Cwd: "/repo", Command: "claude --session-id aaa"})
	srv.sessionRuns.note(&classifier.Worker{ID: "w-1", ProcessID: 42, StartedAt: started.Add(time.Minute), Cwd: "/repo", Command: "claude --resume aaa"})

	ended := srv.endedRunWorker(classifier.Worker{ID: "w-1", Name: "Q3 reports", Cwd: "/repo"})
	if ended.Command != "claude --resume aaa" || !ended.StartedAt.Equal(started.Add(time.Minute)) {
		t.Fatalf("ended worker = %#v", ended)
	}
	if got := srv.structuredProviderForWorker(&ended); got != "claude" {
		t.Fatalf("ended provider = %q", got)
	}
	if shell := srv.endedRunWorker(classifier.Worker{ID: "w-1", Cwd: "/repo", Command: "zsh"}); shell.Command != "zsh" {
		t.Fatalf("a Worker running something kept %q", shell.Command)
	}
	if never := srv.endedRunWorker(classifier.Worker{ID: "w-2", Cwd: "/repo"}); never.Command != "" {
		t.Fatalf("an unrecorded Worker got %q", never.Command)
	}
}
