package connections

import (
	"os/exec"
	"syscall"
)

func cloudParentDeath(cmd *exec.Cmd) { cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL }
