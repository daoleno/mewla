package watcher

import "testing"

// An OpenCode Mewla launched on a session is found by that session again
// after the daemon restarts: the command read off its process keeps
// --session (11-24: it read "opencode", so the restarted daemon never
// followed the Session again and its Model sheet said the service was not
// connected).
func TestOpenCodeProcessKeepsItsSession(t *testing.T) {
	for _, args := range []string{
		"/home/u/.opencode/bin/opencode --session ses_df3d77817ffeKfRAo26OMA6s7w",
		"opencode --session=ses_df3d77817ffeKfRAo26OMA6s7w",
	} {
		if got := workerCommandFromProcess(processInfo{comm: "opencode", args: args}); got != "opencode --session ses_df3d77817ffeKfRAo26OMA6s7w" {
			t.Fatalf("%q read as %q", args, got)
		}
	}
	if got := workerCommandFromProcess(processInfo{comm: "opencode", args: "/home/u/.opencode/bin/opencode"}); got != "opencode" {
		t.Fatalf("a plain OpenCode read as %q", got)
	}
}
