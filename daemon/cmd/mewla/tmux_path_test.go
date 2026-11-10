package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Tests here start and kill their own isolated tmux servers, and some wrap the
// real tmux in a stub that execs the path exec.LookPath("tmux") returned. Drop
// an old pane shim from PATH before any test resolves tmux. An init, not a
// TestMain.
func init() {
	os.Setenv("PATH", withoutOldTmuxShim(os.Getenv("PATH")))
}

// withoutOldTmuxShim drops the `tmux` shim a Mewla before 0.3 put first on
// PATH in its panes (<state>/tmux-guard/bin). Run from such a pane, tests that
// start and kill their own tmux servers must reach the real tmux.
func withoutOldTmuxShim(path string) string {
	var kept []string
	for _, dir := range filepath.SplitList(path) {
		if filepath.Base(dir) == "bin" && filepath.Base(filepath.Dir(dir)) == "tmux-guard" {
			continue
		}
		kept = append(kept, dir)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}
