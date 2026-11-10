package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real-tmux tests here start and kill their own isolated servers.
func TestMain(m *testing.M) {
	os.Setenv("PATH", withoutOldTmuxShim(os.Getenv("PATH")))
	os.Exit(m.Run())
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
