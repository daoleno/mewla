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
	folder := filepath.Join(dir, "a b")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	report := "file://localhost" + strings.ReplaceAll(folder, " ", "%20")
	if _, err := w.CreateSession("", CreateSessionOptions{Cwd: report, Command: "sh", Name: "osc7", Detached: true}); err != nil {
		t.Fatal(err)
	}
	if len(backend.created) != 1 || backend.created[0].cwd != folder {
		t.Fatalf("started outside %q: %+v", folder, backend.created)
	}
}
