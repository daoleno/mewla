package watcher

import (
	"testing"
	"time"
)

// On Windows a Session's `mewla sh` launcher stands in for its process group.
func TestLauncherProcessIsMewlaSh(t *testing.T) {
	for line, want := range map[string]bool{
		`C:\Users\a\AppData\Local\Programs\Mewla\mewla.exe sh -c "exec claude"`: true,
		`"C:\Program Files\Mewla\mewla.exe" sh -c "exec claude"`:                true,
		`C:\Users\a\mewla.exe host --socket x`:                                  false,
		`"C:\Program Files\Mewla\mewla.exe" -state-dir x`:                       false,
		`"C:\Program Files\Mewla\mewla.exe`:                                     false,
	} {
		if got := launcherProcess(processInfo{comm: "mewla", args: line, startedAt: time.Now()}); got != want {
			t.Errorf("launcherProcess(%q) = %v", line, got)
		}
	}
	if launcherProcess(processInfo{comm: "claude", args: `claude.exe sh -c x`, startedAt: time.Now()}) {
		t.Error("a non-mewla program counted as the launcher")
	}
}
