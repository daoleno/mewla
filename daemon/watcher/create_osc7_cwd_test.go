package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The New session sheet may still offer a folder an older daemon read from a
// raw OSC 7 report (file://host/path, percent-encoded). Starting there starts
// in the path, never fails with "must be an absolute path".
func TestCreateSessionStartsInAnOSC7FolderAsAPath(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	script := "#!/bin/sh\n[ \"$1\" = -u ] && shift\nprintf '%s\\n' \"$*\" >> \"$MEWLA_TEST_TMUX_LOG\"\ncase \"$1\" in\n  new-session) printf '%%1\\n' ;;\nesac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MEWLA_TEST_TMUX_LOG", logPath)
	folder := filepath.Join(dir, "a b")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	w := New(0)
	report := "file://localhost" + strings.ReplaceAll(folder, " ", "%20")
	if _, err := w.CreateSession("", CreateSessionOptions{Cwd: report, Command: "sh", Name: "osc7", Detached: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if log := string(raw); !strings.Contains(log, "-c "+folder) || strings.Contains(log, "file:") {
		t.Fatalf("tmux started outside %q:\n%s", folder, log)
	}
}
