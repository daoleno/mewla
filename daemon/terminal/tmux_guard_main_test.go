package terminal

import (
	"os"
	"testing"

	"github.com/daoleno/mewla/daemon/tmuxguard"
)

// The real-tmux tests here start and kill their own isolated servers. Run from
// a guarded Worker pane they must reach the real tmux, not the pane shim.
func TestMain(m *testing.M) {
	os.Setenv("PATH", tmuxguard.WithoutGuard(os.Getenv("PATH")))
	os.Exit(m.Run())
}
