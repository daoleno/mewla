package main

import (
	"runtime"

	"github.com/daoleno/mewla/daemon/work"
)

// agyInstallCommand is Google's official installer for the Antigravity CLI.
func agyInstallCommand(goos string) string {
	return work.AgentInstallCommand(work.WorkerProviderAntigravity, goos)
}

// executorNotReady says, before a Worker is spawned, why an Antigravity launch
// could only fail: agy is not installed. It returns "" for every other agent
// and when agy is installed.
func executorNotReady(command string) string {
	if work.InferWorkerProvider(command) != work.WorkerProviderAntigravity {
		return ""
	}
	if _, err := work.AgentLookPath("agy"); err != nil {
		return "Antigravity (agy) is not installed on this computer. Install it with: " + agyInstallCommand(runtime.GOOS)
	}
	return ""
}
