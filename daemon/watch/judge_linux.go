package watch

import (
	"os/exec"
	"syscall"
)

// A judgment run cannot outlive the daemon that started it.
func bindJudgeToDaemon(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
