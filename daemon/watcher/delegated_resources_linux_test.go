//go:build linux

package watcher

import (
	"os"
	"strings"
	"testing"
)

func TestValidateDelegatedWorkspaceRejectsMemoryBackedFilesystem(t *testing.T) {
	root := "/dev/shm"
	if _, err := os.Stat(root); err != nil {
		t.Skipf("%s unavailable: %v", root, err)
	}
	dir, err := os.MkdirTemp(root, "mewla-workspace-test-")
	if err != nil {
		t.Skipf("cannot create tmpfs fixture: %v", err)
	}
	defer os.RemoveAll(dir)
	if err := validateDelegatedWorkspace(dir); err == nil || !strings.Contains(err.Error(), "memory-backed temporary storage") {
		t.Fatalf("validateDelegatedWorkspace(%q) error = %v", dir, err)
	}
}
