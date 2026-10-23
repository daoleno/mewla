package brain

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// productWorkspaceRoot lists the top-level entries Mewla owns in the Brain
// workspace. Anything else visible at the root is user or tool clutter.
var productWorkspaceRoot = []string{"AGENTS.md", "current.md", "memory.md", "profile.md", routingGuideName, "policies", "playbooks", "worklog"}

func (s *Store) unmanagedWorkspaceEntries() ([]string, error) {
	entries, err := os.ReadDir(s.WorkspacePath())
	if err != nil {
		return nil, err
	}
	unmanaged := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || slices.Contains(productWorkspaceRoot, name) {
			continue
		}
		if entry.IsDir() {
			name += "/"
		}
		unmanaged = append(unmanaged, name)
	}
	sort.Strings(unmanaged)
	return unmanaged, nil
}

// Private notes are read on demand. Budgets keep the always-loaded handoff
// small; memory and profile hold durable facts, not history.
var workspaceNoteBudgets = []struct {
	path   string
	budget int64
}{
	{"current.md", 16 << 10},
	{"memory.md", 32 << 10},
	{"profile.md", 8 << 10},
	{routingGuideName, 4 << 10},
}

func (s *Store) WorkspaceNotes() ([]WorkspaceNote, error) {
	notes := make([]WorkspaceNote, 0, len(workspaceNoteBudgets))
	for _, spec := range workspaceNoteBudgets {
		var size int64
		info, err := os.Stat(filepath.Join(s.WorkspacePath(), spec.path))
		switch {
		case err == nil:
			size = info.Size()
		case !errors.Is(err, os.ErrNotExist):
			return nil, err
		}
		notes = append(notes, WorkspaceNote{Path: spec.path, Bytes: size, BudgetBytes: spec.budget, OverBudget: size > spec.budget})
	}
	return notes, nil
}
