//go:build linux

package watcher

import (
	"fmt"
	"syscall"
)

const (
	tmpfsMagic     = 0x01021994
	ramfsMagic     = 0x858458f6
	hugetlbfsMagic = 0x958458f6
)

func newDelegatedResourceManager(owner string) delegatedResourceManager {
	m, err := newPortableDelegatedResourceManager(owner)
	if err != nil {
		return unavailableDelegatedResourceManager{reason: err.Error()}
	}
	return m
}

func validateDelegatedWorkspace(cwd string) error {
	resolved, err := validateDelegatedWorkspacePath(cwd)
	if err != nil {
		return err
	}
	if resolved == "" {
		return nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(resolved, &stat); err != nil {
		return nil
	}
	switch uint64(stat.Type) {
	case tmpfsMagic, ramfsMagic, hugetlbfsMagic:
		return fmt.Errorf("delegated Zen Worker cwd %q is on memory-backed temporary storage; use a durable workspace such as $ZEN_WORKTREE_ROOT (default ~/.zen/worktrees)", cwd)
	default:
		return nil
	}
}
