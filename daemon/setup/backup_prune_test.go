package setup

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// Each config write backs up the file it replaces; only the newest such
// backup stays. A copy made by hand under another name is left alone.
func TestAConfigWriteKeepsOnlyItsNewestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	if err := os.WriteFile(path, []byte("[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	byHand := path + ".bak.1787250565"
	if err := os.WriteFile(byHand, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var last string
	for i, name := range []string{"a", "b", "c"} {
		backup, err := AddExecutor(path, work.Executor{Name: name, Command: "claude"}, time.Unix(int64(100+i), 0))
		if err != nil || backup == "" {
			t.Fatalf("write %d: %q %v", i, backup, err)
		}
		last = backup
	}
	result, err := writeExecutorsConfig(configWriteRequest{Path: path, Selected: []selectedExecutor{{ID: "claude", Provider: "claude", Existing: work.Executor{Command: "claude"}}}, Now: time.Unix(200, 0)})
	if err != nil || result.BackupPath == "" || result.BackupPath == last {
		t.Fatalf("setup write: %+v %v", result, err)
	}
	left, _ := filepath.Glob(path + ".bak.*")
	sort.Strings(left)
	want := []string{byHand, result.BackupPath}
	sort.Strings(want)
	if len(left) != 2 || left[0] != want[0] || left[1] != want[1] {
		t.Fatalf("backups left = %v, want %v", left, want)
	}
}
