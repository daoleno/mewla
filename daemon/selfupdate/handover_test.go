package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBinary writes an executable shell script. `version --json` answers with
// version; anything else runs body (what a daemon start would do).
func fakeBinary(t *testing.T, path, version, body string) {
	t.Helper()
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo '{\"version\":\"" + version + "\"}'; exit 0; fi\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

type handoverFixture struct {
	dir, installed, state string
	database              string
}

func newHandoverFixture(t *testing.T) handoverFixture {
	t.Helper()
	dir := t.TempDir()
	f := handoverFixture{dir: dir, installed: filepath.Join(dir, "mewla"), state: filepath.Join(dir, "state")}
	fakeBinary(t, f.installed, "1.0.0", "exit 0")
	f.database = filepath.Join(f.state, "calendar", "calendar.db")
	if err := os.MkdirAll(filepath.Dir(f.database), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.database, []byte("schema-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f handoverFixture) read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (f handoverFixture) handover(t *testing.T, staged string, healthy func() error) Handover {
	return Handover{
		Installed: f.installed, Staged: staged, Version: "2.0.0", StateDir: f.state, BrainRoot: filepath.Join(f.dir, "brain"),
		Gate: 3 * time.Second, StopGrace: 2 * time.Second, Healthy: healthy,
	}
}

func stage(t *testing.T, f handoverFixture, body string) string {
	if runtime.GOOS == "windows" {
		t.Skip("the fake versions are shell scripts; the Windows handover is proven on a Windows machine")
	}
	t.Helper()
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo '{\"version\":\"2.0.0\"}'; exit 0; fi\n" + body + "\n"
	staged, err := Stage(f.installed, []byte(script), "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return staged
}

func TestStageRefusesBinariesThatDoNotRunOrMisreportTheirVersion(t *testing.T) {
	f := newHandoverFixture(t)
	for name, script := range map[string]string{
		"not executable code": "not a program",
		"wrong version":       "#!/bin/sh\necho '{\"version\":\"9.9.9\"}'",
		"crashes on launch":   "#!/bin/sh\nexit 1",
	} {
		if _, err := Stage(f.installed, []byte(script), "2.0.0"); err == nil {
			t.Fatalf("%s: Stage accepted it", name)
		}
		if _, err := os.Stat(f.installed + StagedSuffix); !os.IsNotExist(err) {
			t.Fatalf("%s: staged file left behind", name)
		}
	}
	if got := f.read(t, f.installed); !strings.Contains(got, "1.0.0") {
		t.Fatalf("installed binary changed: %q", got)
	}
}

func TestHandoverAdoptsAnAnsweringVersionAndKeepsThePreviousOne(t *testing.T) {
	f := newHandoverFixture(t)
	ready := filepath.Join(f.dir, "ready")
	// The "daemon" writes the database, announces itself and waits for SIGTERM.
	staged := stage(t, f, "echo schema-2 > '"+f.database+"'; touch '"+ready+"'; trap 'exit 0' TERM; while :; do sleep 0.1; done")
	h := f.handover(t, staged, func() error { _, err := os.Stat(ready); return err })
	outcome := h.Run(context.Background())
	if !outcome.Adopted {
		t.Fatalf("not adopted: %+v", outcome)
	}
	if got := f.read(t, f.installed); !strings.Contains(got, "2.0.0") {
		t.Fatalf("installed is not the new version: %q", got)
	}
	if got := f.read(t, f.installed+PrevSuffix); !strings.Contains(got, "1.0.0") {
		t.Fatalf("previous version not kept: %q", got)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("staged binary should have moved into place")
	}
	if got := f.read(t, f.database); got != "schema-2\n" {
		t.Fatalf("adopting must keep the new version's data, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.state, backupDir)); !os.IsNotExist(err) {
		t.Fatal("backup should be discarded once the version is adopted")
	}
}

func TestHandoverRollsBackWhenTheNewVersionExitsAtOnce(t *testing.T) {
	f := newHandoverFixture(t)
	// It migrates the database (new schema plus a write-ahead log), then dies.
	staged := stage(t, f, "echo schema-2 > '"+f.database+"'; echo log > '"+f.database+"-wal'; exit 1")
	h := f.handover(t, staged, func() error { return os.ErrNotExist })
	outcome := h.Run(context.Background())
	assertRolledBack(t, f, staged, outcome, true)
}

func TestHandoverRollsBackWhenTheNewVersionHangs(t *testing.T) {
	f := newHandoverFixture(t)
	staged := stage(t, f, "echo schema-2 > '"+f.database+"'; exec sleep 60")
	h := f.handover(t, staged, func() error { return os.ErrNotExist })
	h.Gate = 700 * time.Millisecond
	start := time.Now()
	outcome := h.Run(context.Background())
	if time.Since(start) > 10*time.Second {
		t.Fatalf("a hung child was not cut off: %s", time.Since(start))
	}
	assertRolledBack(t, f, staged, outcome, true)
}

func TestHandoverStoppedMidwayRollsBackWithoutBlamingTheVersion(t *testing.T) {
	f := newHandoverFixture(t)
	staged := stage(t, f, "echo schema-2 > '"+f.database+"'; trap 'exit 0' TERM; while :; do sleep 0.1; done")
	ctx, cancel := context.WithCancel(context.Background())
	h := f.handover(t, staged, func() error { return os.ErrNotExist })
	time.AfterFunc(500*time.Millisecond, cancel)
	outcome := h.Run(ctx)
	assertRolledBack(t, f, staged, outcome, false)
}

func TestHandoverDoesNotStartWithoutABackup(t *testing.T) {
	f := newHandoverFixture(t)
	ran := filepath.Join(f.dir, "ran")
	staged := stage(t, f, "touch '"+ran+"'; exit 1")
	if err := os.Chmod(f.state, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(f.state, 0o700)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	h := f.handover(t, staged, func() error { return os.ErrNotExist })
	outcome := h.Run(context.Background())
	if outcome.Adopted || outcome.Bad || !strings.Contains(outcome.Reason, "back up") {
		t.Fatalf("outcome = %+v", outcome)
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("the new version must not run when its data cannot be backed up")
	}
}

func TestRecoverInterruptedRestoresDatabasesAfterAPowerCut(t *testing.T) {
	f := newHandoverFixture(t)
	if err := backupDatabases(f.state, filepath.Join(f.dir, "brain")); err != nil {
		t.Fatal(err)
	}
	// The new version migrated, then the machine lost power: no Run to clean up.
	os.WriteFile(f.database, []byte("schema-2"), 0o600)
	os.WriteFile(f.database+"-wal", []byte("log"), 0o600)
	os.WriteFile(f.installed+StagedSuffix, []byte("x"), 0o755)

	t.Setenv(ChildEnv, "1")
	if err := RecoverInterrupted(f.state, f.installed); err != nil {
		t.Fatal(err)
	}
	if got := f.read(t, f.database); got != "schema-2" {
		t.Fatal("the trial child must not run recovery")
	}
	t.Setenv(ChildEnv, "")
	if err := RecoverInterrupted(f.state, f.installed); err != nil {
		t.Fatal(err)
	}
	if got := f.read(t, f.database); got != "schema-1" {
		t.Fatalf("database = %q, want the backed-up schema-1", got)
	}
	for _, leftover := range []string{f.database + "-wal", f.installed + StagedSuffix, filepath.Join(f.state, backupDir)} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone", leftover)
		}
	}
	if err := RecoverInterrupted(f.state, f.installed); err != nil {
		t.Fatalf("recovery with nothing to recover: %v", err)
	}
}

func TestFailureRecord(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LastFailure(dir); ok {
		t.Fatal("no failure recorded yet")
	}
	if err := RecordFailure(dir, Failure{Version: "2.0.0", Reason: "exited", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if failure, ok := LastFailure(dir); !ok || failure.Version != "2.0.0" {
		t.Fatalf("failure = %+v %v", failure, ok)
	}
}

func assertRolledBack(t *testing.T, f handoverFixture, staged string, outcome Outcome, wantBad bool) {
	t.Helper()
	if outcome.Adopted || outcome.Bad != wantBad || outcome.Reason == "" {
		t.Fatalf("outcome = %+v, want rolled back (bad=%v)", outcome, wantBad)
	}
	if got := f.read(t, f.installed); !strings.Contains(got, "1.0.0") {
		t.Fatalf("installed binary must be untouched: %q", got)
	}
	if got := f.read(t, f.database); got != "schema-1" {
		t.Fatalf("database = %q, want schema-1 restored", got)
	}
	for _, leftover := range []string{f.database + "-wal", staged, f.installed + PrevSuffix, filepath.Join(f.state, backupDir)} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after a rollback", leftover)
		}
	}
}

func TestPromoteFallsBackToACopyWhenTheFilesystemCannotLink(t *testing.T) {
	f := newHandoverFixture(t)
	staged := stage(t, f, "exit 0")
	defer func(previous func(string, string) error) { linkFile = previous }(linkFile)
	linkFile = func(string, string) error { return os.ErrPermission }
	if err := promote(f.installed, staged); err != nil {
		t.Fatal(err)
	}
	if got := f.read(t, f.installed); !strings.Contains(got, "2.0.0") {
		t.Fatalf("installed = %q", got)
	}
	prev := f.installed + PrevSuffix
	if got := f.read(t, prev); !strings.Contains(got, "1.0.0") {
		t.Fatalf("previous = %q", got)
	}
	if info, err := os.Stat(prev); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the kept previous binary must stay executable: %v %v", info, err)
	}
}

func TestFailureCooldown(t *testing.T) {
	now := time.Now()
	if !(Failure{Version: "2.0.0"}).HoldsAutomatic(now) {
		t.Fatal("a version that failed to run is never retried")
	}
	cooling := Failure{Version: "2.0.0", RetryAfter: now.Add(time.Hour)}
	if cooling.Permanent() || !cooling.HoldsAutomatic(now) || cooling.HoldsAutomatic(now.Add(2*time.Hour)) {
		t.Fatal("a transient failure holds the automatic path only until its retry time")
	}
}
