//go:build !linux

package watch

import "os/exec"

// Without parent-death signals the run is bounded by its timeout alone.
func bindJudgeToDaemon(*exec.Cmd) {}
