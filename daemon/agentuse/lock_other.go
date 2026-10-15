//go:build !unix

package agentuse

func lockedByAnotherProcess(string, int64, int64) bool { return false }
