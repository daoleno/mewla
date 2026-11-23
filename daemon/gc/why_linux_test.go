//go:build linux

package gc

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

const whyOwner = "0123456789abcdef0123456789abcdef"

// whyFixture is a state root with one Session: its lease and its marked
// scratch.
func whyFixture(t *testing.T) (root, unit, scratch string) {
	t.Helper()
	root = t.TempDir()
	unit = "mewla-worker-" + whyOwner + "-" + strings.Repeat("ab", 16) + ".scope"
	scratch = filepath.Join(root, "t", ScratchName(unit))
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, scratchMarker), []byte(unit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	leases := filepath.Join(root, "run", "worker-resources", whyOwner)
	if err := os.MkdirAll(leases, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(workerproc.Lease{Version: 1, ResourceID: unit, StartedAt: time.Now()})
	if err := os.WriteFile(filepath.Join(leases, unit+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, unit, scratch
}

// startStripped starts a copy of sleep that lives in dir, as `setsid env -i`
// would: a new session and no environment at all.
func startStripped(t *testing.T, dir, name string, cwd string) int {
	t.Helper()
	exe := filepath.Join(dir, name)
	raw, err := os.ReadFile("/usr/bin/sleep")
	if err != nil {
		t.Skip("no /usr/bin/sleep to copy")
	}
	if err := os.WriteFile(exe, raw, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "30")
	cmd.Env, cmd.Dir = []string{}, cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	// Wait until the exec has happened: the exe link names the copy.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if target, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "exe")); target == exe {
			break
		}
	}
	return cmd.Process.Pid
}

func chainText(why Why) string {
	var parts []string
	for _, link := range why.Chain {
		parts = append(parts, link.Kind+":"+link.ID+":"+link.State+":"+link.By)
	}
	return strings.Join(parts, " | ")
}

func TestWhyFollowsAStrippedProcessToItsSessionAndWork(t *testing.T) {
	root, unit, scratch := whyFixture(t)
	pid := startStripped(t, scratch, "mewla", "/")
	lookup := WhyLookup{Root: root,
		Session: func(u string) (WhySession, bool) {
			return WhySession{ID: "mh-1", Name: "Release gate", State: "running"}, u == unit
		},
		Work: func(session string) (WhyLink, bool) {
			return WhyLink{ID: "w-1", Name: "Release 0.4.26", State: "open"}, session == "mh-1"
		},
	}
	why, err := WhyProcess(lookup, pid)
	if err != nil {
		t.Fatal(err)
	}
	want := []WhyLink{
		{Kind: "process", ID: strconv.Itoa(pid), Name: "mewla"},
		{Kind: "scratch", ID: scratch, By: "its executable is there"},
		{Kind: "session", ID: "mh-1", Name: "Release gate", State: "running", By: "its marker names the Session"},
		{Kind: "work", ID: "w-1", Name: "Release 0.4.26", State: "open"},
	}
	if !why.Mewla || len(why.Chain) != len(want) {
		t.Fatalf("chain %s", chainText(why))
	}
	for i := range want {
		if why.Chain[i] != want[i] {
			t.Fatalf("link %d = %+v, want %+v\n%s", i, why.Chain[i], want[i], chainText(why))
		}
	}
	// Without a daemon to ask, the chain stops at the Session's unit.
	why, err = WhyProcess(WhyLookup{Root: root}, pid)
	if err != nil {
		t.Fatal(err)
	}
	if last := why.Chain[len(why.Chain)-1]; last.Kind != "session" || last.ID != unit || last.State != "lease held" {
		t.Fatalf("chain %s", chainText(why))
	}
	// A daemon that has no live Session for the unit: the Session ended.
	why, err = WhyProcess(WhyLookup{Root: root, Session: func(string) (WhySession, bool) { return WhySession{}, false }}, pid)
	if err != nil {
		t.Fatal(err)
	}
	if last := why.Chain[len(why.Chain)-1]; last.ID != unit || last.State != "ended; lease not released yet" {
		t.Fatalf("chain %s", chainText(why))
	}
}

func TestWhyNamesAProcessRunningInAScratchByItsWorkingDirectory(t *testing.T) {
	root, unit, scratch := whyFixture(t)
	elsewhere := t.TempDir()
	pid := startStripped(t, elsewhere, "tool", scratch)
	why, err := WhyProcess(WhyLookup{Root: root}, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(why.Chain) != 3 || why.Chain[1].By != "its working directory is there" || why.Chain[2].ID != unit {
		t.Fatalf("chain %s", chainText(why))
	}
}

func TestWhySaysWhenNoSessionHoldsAProcess(t *testing.T) {
	root, _, _ := whyFixture(t)
	pid := startStripped(t, t.TempDir(), "tool", "/")
	why, err := WhyProcess(WhyLookup{Root: root}, pid)
	if err != nil {
		t.Fatal(err)
	}
	if why.Mewla || len(why.Chain) != 2 || why.Chain[1].Kind != "unheld" {
		t.Fatalf("chain %s", chainText(why))
	}
	if _, err := WhyProcess(WhyLookup{Root: root}, 1<<30); err != ErrNoProcess {
		t.Fatalf("missing pid: %v", err)
	}
}

func TestWhyPathFollowsEachKindOfPlace(t *testing.T) {
	root, unit, scratch := whyFixture(t)
	file := filepath.Join(scratch, "out", "report.txt")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := AddKeep(root, filepath.Join(scratch, "out"), unit, now.Add(48*time.Hour), "release notes", now); err != nil {
		t.Fatal(err)
	}
	unmarked := filepath.Join(root, "t", "loose")
	if err := os.MkdirAll(unmarked, 0o700); err != nil {
		t.Fatal(err)
	}
	trashed := filepath.Join(root, "t", "old")
	if err := os.MkdirAll(trashed, 0o700); err != nil {
		t.Fatal(err)
	}
	item, err := Trash(root, KindUnmarked, trashed, unit, ReasonUnmarked, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path  string
		kinds string
		check func(Why) bool
	}{
		{file, "path scratch session", func(w Why) bool {
			return strings.HasPrefix(w.Chain[0].State, "kept until ") && w.Chain[2].ID == unit
		}},
		{scratch, "path session", nil},
		{unmarked, "path", func(w Why) bool { return strings.HasPrefix(w.Chain[0].State, "unmarked;") }},
		{item.Path, "path trash session", func(w Why) bool { return w.Chain[1].Name == trashed && w.Chain[1].By == ReasonUnmarked }},
		{filepath.Join(CacheDir(root), "go"), "path cache", nil},
		{filepath.Join(root, "worktrees", "repo-x"), "path worktree", nil},
		{filepath.Join(root, "brain"), "path state", nil},
		{t.TempDir(), "path outside", func(w Why) bool { return !w.Mewla }},
	} {
		why, err := WhyPath(WhyLookup{Root: root}, c.path)
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, link := range why.Chain {
			kinds = append(kinds, link.Kind)
		}
		if strings.Join(kinds, " ") != c.kinds || (c.check != nil && !c.check(why)) {
			t.Fatalf("%s: %s", c.path, chainText(why))
		}
	}
}
