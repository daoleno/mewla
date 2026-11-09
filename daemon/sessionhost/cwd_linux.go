package sessionhost

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// processCwd is the working directory of the Session's foreground process,
// the one a user would say the Session is in.
func processCwd(ptmx *os.File, pid int) string {
	if pgrp, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP); err == nil && pgrp > 0 {
		if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pgrp)); err == nil {
			return cwd
		}
	}
	cwd, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	return cwd
}
