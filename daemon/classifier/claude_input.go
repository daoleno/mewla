package classifier

import "strings"

// ClaudeSubmittedInput is the text Claude Code submits, and records in its
// transcript, for text typed or pasted into its prompt: the prompt editor
// turns every tab into four spaces (Claude Code 2.1.310 to 2.1.332,
// value.replaceAll("\t", "    ") on every change). Input whose bytes hold a
// tab is recorded with these bytes, never its own.
func ClaudeSubmittedInput(text string) string {
	return strings.ReplaceAll(text, "\t", "    ")
}
