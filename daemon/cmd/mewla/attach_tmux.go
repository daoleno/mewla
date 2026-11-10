package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/daoleno/mewla/daemon/tmuxsocket"
)

// tmuxAttachArgs is the tmux command line that shows a Session's pane: attach
// to the tmux session that holds it, then bring its window and pane forward.
// Inside tmux on that same server it switches this client instead, as tmux
// does not nest.
func tmuxAttachArgs(target attachTmux, switchClient bool) []string {
	args := tmuxsocket.ClientArgs(target.Socket)
	verb := "attach-session"
	if switchClient {
		verb = "switch-client"
	}
	return append(args, verb, "-t", target.Session,
		";", "select-window", "-t", target.Pane,
		";", "select-pane", "-t", target.Pane)
}

// insideSameTmux reports whether this process runs in a tmux client of the
// server that holds target.
func insideSameTmux(target attachTmux, tmuxEnv string) bool {
	current := tmuxSocketFromEnvironment(tmuxEnv)
	if current == "" {
		return false
	}
	want := target.Socket
	if want == "" {
		want = defaultTmuxSocket(os.Getenv("TMUX_TMPDIR"), os.Getuid())
	}
	return filepath.Clean(current) == filepath.Clean(want)
}

// defaultTmuxSocket is where tmux keeps the default server's socket.
func defaultTmuxSocket(tmpdir string, uid int) string {
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	return filepath.Join(tmpdir, fmt.Sprintf("tmux-%d", uid), "default")
}

// tmuxDetachNote says how to leave tmux: Ctrl-\ is not its detach key and
// reaches the pane's program, so the first line names the real one.
func tmuxDetachNote(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "C-b"
	}
	return fmt.Sprintf("this Session runs in tmux: detach with %s then d, not Ctrl-\\; it keeps running", prefix)
}

// runTmuxAttach opens a Session still in tmux with tmux itself and returns when
// tmux does.
func runTmuxAttach(entry attachEntry) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("%s is in tmux, which this computer does not run", entry.Name)
	}
	target := *entry.Tmux
	if os.Getenv("TMUX") != "" && !insideSameTmux(target, os.Getenv("TMUX")) {
		return fmt.Errorf("%s is in a different tmux server than the one you are in; run mewla attach outside tmux", entry.Name)
	}
	switchClient := os.Getenv("TMUX") != ""
	prefix, _ := exec.Command("tmux", append(tmuxsocket.ClientArgs(target.Socket), "show-options", "-gv", "prefix")...).Output()
	if switchClient {
		fmt.Fprintf(os.Stdout, "Switching this tmux client to %s (%s); switch back with your tmux keys.\r\n", entry.Name, entry.ID)
	} else {
		fmt.Fprintf(os.Stdout, "Attaching to %s (%s): %s.\r\n", entry.Name, entry.ID, tmuxDetachNote(string(prefix)))
	}
	cmd := exec.Command("tmux", tmuxAttachArgs(target, switchClient)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
