//go:build !linux && !darwin && !windows

package watcher

func newDelegatedResourceManager(string) delegatedResourceManager {
	return unavailableDelegatedResourceManager{reason: "process ownership tracking is not implemented on this platform"}
}

func validateDelegatedWorkspace(string) error { return nil }
