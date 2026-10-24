package main

import (
	"os"

	"github.com/daoleno/mewla/daemon/tmuxguard"
)

// Tests here start and kill their own isolated tmux servers, and some wrap the
// real tmux in a stub that execs the path exec.LookPath("tmux") returned. Run
// from a guarded Worker pane that path is the pane shim, and the guard sees
// the stub that wraps it as a wrapper looping back into it and refuses. Drop
// the guard from PATH before any test resolves tmux. An init, not a TestMain,
// because the browserfixture build already has one.
func init() {
	os.Setenv("PATH", tmuxguard.WithoutGuard(os.Getenv("PATH")))
}
