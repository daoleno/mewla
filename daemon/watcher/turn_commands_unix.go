//go:build !windows

package watcher

import (
	"errors"
	"syscall"
)

// processGroupsAreReal: a command's group is its process group, which one
// signal reaches whole.
const processGroupsAreReal = true

// signalCommandGroup sends SIGTERM (SIGKILL when final) to the command's
// process group. Its id is the leader's pid, which names this group only
// while a member of it is left, so the final signal goes only to a group with
// one of its own members still there.
func signalCommandGroup(group turnCommandGroup, final bool) error {
	signal := syscall.SIGTERM
	if final {
		if !commandGroupAlive(group) {
			return nil
		}
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(-group.leader.pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// commandGroupAlive reports whether any process of the command is still
// running.
func commandGroupAlive(group turnCommandGroup) bool {
	if err := syscall.Kill(-group.leader.pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	for _, member := range group.members {
		if processStillThere(member) {
			return true
		}
	}
	return false
}
