package main

import (
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/platform"
)

// workerProcessArgs is the command line the Worker's agent process runs: the
// flags it was started with, which the Worker's recorded command (its
// identity) may leave out.
func workerProcessArgs(worker *classifier.Worker) string {
	if worker == nil || worker.ProcessID <= 0 {
		return ""
	}
	args, err := platform.ProcessCommandLine(worker.ProcessID)
	if err != nil {
		return ""
	}
	return args
}
