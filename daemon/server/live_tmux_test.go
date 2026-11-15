package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// liveTmuxSocket is the private tmux server the opt-in live tests drive a real
// agent TUI in: an explicit socket under the test's temporary directory, never
// the person's own tmux server.
var liveTmuxSocket = filepath.Join(os.TempDir(), fmt.Sprintf("mewla-live-tmux-%d.sock", os.Getpid()))

func liveTmux(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-S", liveTmuxSocket}, args...)...)
}
