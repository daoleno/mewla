package sessionhost

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// foreground is the process group in the Session's foreground, the one a
// user would say the Session is running; the program itself when that cannot
// be read.
func foreground(ptmx *os.File, pid int) int {
	if pgrp, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP); err == nil && pgrp > 0 {
		return pgrp
	}
	return pid
}

// processCwd is the working directory of the Session's foreground process.
func processCwd(ptmx *os.File, pid int) string {
	if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", foreground(ptmx, pid))); err == nil {
		return cwd
	}
	cwd, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	return cwd
}

// processCommand names the Session's foreground process the way tmux's
// pane_current_command does: its argv[0] (which a program may rewrite, as
// node does for process.title), read from /proc/<pgrp>/cmdline.
func processCommand(ptmx *os.File, pid int) string {
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", foreground(ptmx, pid)))
	if err != nil {
		return ""
	}
	if cut := bytes.IndexByte(cmdline, 0); cut >= 0 {
		cmdline = cmdline[:cut]
	}
	return commandName(string(cmdline))
}
