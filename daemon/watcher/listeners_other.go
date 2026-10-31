//go:build !linux

package watcher

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/daoleno/mewla/daemon/procspawn"
)

// listListeningSockets lists TCP sockets in LISTEN state and their owning
// process through lsof's field output (-F), which is a stable machine format.
// Like any unprivileged lister it sees only the user's own processes.
func listListeningSockets() ([]listeningSocket, error) {
	out, err := procspawn.Command("lsof listeners", procspawn.SystemTool("lsof"), "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pfdn").Output()
	if err != nil {
		// lsof exits 1 when nothing matches or some file could not be read,
		// and still prints what it found.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("lsof listening sockets: %w", err)
		}
	}
	return parseLsofListeningSockets(string(out)), nil
}
