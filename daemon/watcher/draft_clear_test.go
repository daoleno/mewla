package watcher

import (
	"strings"
	"testing"
)

// composer is Claude's and Codex's multi-line input as measured in their
// terminals (claude 2.1, codex 0.167): Ctrl-U clears the cursor's line,
// Backspace at a line's start joins it to the line above.
type composer struct{ lines []string }

func (c *composer) key(key string) {
	last := len(c.lines) - 1
	switch key {
	case "C-u":
		c.lines[last] = ""
	case "BSpace":
		if c.lines[last] != "" {
			c.lines[last] = c.lines[last][:len(c.lines[last])-1]
		} else if last > 0 {
			c.lines = c.lines[:last]
		}
	}
}

// A Brain activation pasted but left unsent, then Chat's message: Ctrl-U
// alone kept "Brain Host activation contract:" and the person's message was
// submitted joined to it, so the timeline hid both it and Brain's answer as
// a private activation (codex Brain after mewla brain use, 2026-11-16).
func TestChatClearsEveryLineOfAnUnsentDraft(t *testing.T) {
	for _, draft := range []string{
		brainActivationDraft,
		"one line",
		"",
		strings.Repeat("a draft line\n", draftClearLines),
	} {
		c := &composer{lines: strings.Split(draft, "\n")}
		for _, key := range draftClearKeys() {
			c.key(key)
		}
		if got := strings.Join(c.lines, "\n"); got != "" {
			t.Fatalf("draft %q left %q in the composer", draft, got)
		}
	}

	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	identity := testSessionInputIdentity("codex")
	if _, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), identity.Command, "Hello from Chat", ""); err != nil {
		t.Fatal(err)
	}
	queue := io.queues[0]
	paste := indexOf(queue, "paste")
	clears := 0
	for _, arg := range queue[:paste] {
		if arg == "BSpace" {
			clears++
		}
	}
	if clears != draftClearLines {
		t.Fatalf("the queue clears %d extra lines before the paste, want %d: %q", clears, draftClearLines, queue)
	}
}

const brainActivationDraft = "Brain Host activation contract:\nRead AGENTS.md before continuing, even in a resumed Session."
