//go:build windows

package launchsh

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/daoleno/mewla/daemon/platform"
)

// command finds argv[0] on the script's PATH and builds its command line:
// an .exe gets Go's quoting, a .cmd shim (npm's) runs through cmd.exe with
// the arguments platform.ResolveProgram allows. A login shell asked for by
// itself (`/bin/sh -i -l`, a Shell Session) is the user's Windows shell,
// PowerShell, unless that name is on PATH (Git Bash).
func command(argv, env []string) (*exec.Cmd, error) {
	path := envValue(env, "PATH")
	app, line, err := platform.ResolveProgram(argv, path)
	if err != nil && posixShell(argv[0]) {
		argv = []string{"powershell.exe", "-NoLogo"}
		app, line, err = platform.ResolveProgram(argv, path)
	}
	if err != nil {
		return nil, err
	}
	return &exec.Cmd{Path: app, Args: argv, SysProcAttr: &syscall.SysProcAttr{CmdLine: line}}, nil
}

func posixShell(program string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(filepath.ToSlash(program))), ".exe")
	return name == "sh" || name == "bash" || name == "zsh"
}
