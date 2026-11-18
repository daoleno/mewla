//go:build unix

package listenhandoff

import (
	"os"

	"golang.org/x/sys/unix"
)

const supported = true

func clearCloseOnExec(file *os.File) bool {
	_, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, 0)
	return err == nil
}

func markCloseOnExec(file *os.File) {
	unix.CloseOnExec(int(file.Fd()))
}
