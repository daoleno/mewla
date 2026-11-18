package watcher

import (
	"errors"
	"testing"
)

func TestProviderCommandIsOnlyCompaction(t *testing.T) {
	for _, tc := range []struct {
		command, text string
		ok            bool
	}{
		{"claude --model claude-sonnet-5-5", "/compact", true},
		{"claude", "/compact keep the open questions", true},
		{"codex", "/compact", true},
		{"pi", "/compact", true},
		{"claude", "/clear", false},
		{"claude", "/compact\nrm -rf x", false},
		{"claude", "compact", false},
		{"cursor-agent", "/compact", false},
		{"claude", "", false},
	} {
		got, err := ValidateProviderCommand(tc.command, tc.text)
		if tc.ok && (err != nil || got == "") {
			t.Fatalf("%q to %q refused: %v", tc.text, tc.command, err)
		}
		if !tc.ok && !errors.Is(err, ErrProviderCommandRefused) {
			t.Fatalf("%q to %q allowed (%q, %v)", tc.text, tc.command, got, err)
		}
	}
}
