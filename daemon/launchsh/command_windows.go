//go:build windows

package launchsh

import (
	"os/exec"
	"syscall"

	"github.com/daoleno/mewla/daemon/platform"
)

// command finds argv[0] on the script's PATH and builds its command line:
// an .exe gets Go's quoting, a .cmd shim (npm's) runs through cmd.exe with
// the arguments platform.ResolveProgram allows. The login shell a Shell
// Session asks for (`/bin/sh -i -l`) is PowerShell on Windows, even with Git
// Bash on PATH: PowerShell 7 (pwsh) when it is installed, else the Windows
// PowerShell every Windows has. A program named by itself, `bash` included,
// is that program.
func command(argv, env []string) (*exec.Cmd, error) {
	path := envValue(env, "PATH")
	if argv[0] == "/bin/sh" {
		argv = windowsShell(path)
	}
	app, line, err := platform.ResolveProgram(argv, path)
	if err != nil {
		return nil, err
	}
	return &exec.Cmd{Path: app, Args: argv, SysProcAttr: &syscall.SysProcAttr{CmdLine: line}}, nil
}

// windowsShell is the person's interactive shell: pwsh when found on PATH,
// powershell.exe otherwise.
func windowsShell(path string) []string {
	if _, _, err := platform.ResolveProgram([]string{"pwsh.exe"}, path); err == nil {
		return []string{"pwsh.exe", "-NoLogo"}
	}
	return []string{"powershell.exe", "-NoLogo"}
}
