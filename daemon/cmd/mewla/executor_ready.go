package main

import (
	"runtime"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/work"
)

// agyInstallCommand is Google's official installer for the Antigravity CLI
// (antigravity.google/download): a per-user install, in ~/.local/bin on macOS
// and Linux and in %LOCALAPPDATA%\agy\bin on Windows (PowerShell).
func agyInstallCommand(goos string) string {
	if goos == "windows" {
		return "irm https://antigravity.google/cli/install.ps1 | iex"
	}
	return "curl -fsSL https://antigravity.google/cli/install.sh | bash"
}

// executorNotReady says, before a Worker is spawned, why an Antigravity launch
// could only stop at a screen nobody is watching: agy is not installed, or has
// no sign-in. It returns "" for every other agent and when agy can start. A
// sign-in is the person's own step in a browser, so the words name it and
// Mewla never types it.
func executorNotReady(command string) string {
	if work.InferWorkerProvider(command) != work.WorkerProviderAntigravity {
		return ""
	}
	if _, err := hostdeps.LookPath("agy"); err != nil {
		return "Antigravity (agy) is not installed on this computer. Install it with: " + agyInstallCommand(runtime.GOOS)
	}
	if brain.ExecutorAuth("agy") == brain.HostAuthUnauthenticated {
		return "Antigravity (agy) is not signed in on this computer. Run agy in a terminal once, choose Google OAuth and finish signing in on the page it opens; the Worker can start after that."
	}
	return ""
}
