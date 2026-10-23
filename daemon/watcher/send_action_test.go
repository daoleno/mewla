package watcher

import (
	"strings"
	"testing"
)

func TestSendActionRejectsRemovedActionsBeforeTouchingTmux(t *testing.T) {
	// A zero Watcher has no tmux, so reaching past the action switch would panic.
	w := &Watcher{}
	for _, action := range []string{"approve", "reject", "show_diff", "run_tests", "git_status", ""} {
		err := w.SendAction("%1", action)
		if err == nil || !strings.Contains(err.Error(), "unknown action") {
			t.Fatalf("SendAction(%q) = %v, want unknown action error", action, err)
		}
	}
}
