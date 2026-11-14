package watcher

import "testing"

// A first message is held only for a command whose agent UI has a known input
// prompt; a shell or an unknown program has none.
func TestHoldsFirstMessage(t *testing.T) {
	for command, want := range map[string]bool{
		"claude --dangerously-skip-permissions": true,
		"codex":                                 true,
		"opencode":                              true,
		"bash":                                  false,
		"":                                      false,
		"python3 serve.py":                      false,
	} {
		if got := HoldsFirstMessage(command); got != want {
			t.Fatalf("HoldsFirstMessage(%q) = %v, want %v", command, got, want)
		}
	}
}
