//go:build windows

package launchsh

import (
	"os/exec"
	"syscall"

	"github.com/daoleno/mewla/daemon/platform"
)

// command finds argv[0] on the script's PATH and builds its command line:
// an .exe gets Go's quoting, a .cmd shim (npm's) runs through cmd.exe with
// the arguments platform.ResolveProgram allows.
func command(argv, env []string) (*exec.Cmd, error) {
	app, line, err := platform.ResolveProgram(argv, envValue(env, "PATH"))
	if err != nil {
		return nil, err
	}
	return &exec.Cmd{Path: app, Args: argv, SysProcAttr: &syscall.SysProcAttr{CmdLine: line}}, nil
}
