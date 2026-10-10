package shellquote

import (
	"os/exec"
	"testing"
)

func TestWordRoundTripsThroughSh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	values := []string{
		"", "plain", "/abs/path", "with space", "it's", `back\slash`, "$HOME", "`id`",
		"a;b", "a&b", "a|b", "a\nb", "*", "?", "[x]", "{a,b}", "(x)", "<x>", "!x", "#x",
		`{"mcpServers":{"x":{"command":"mewla"}}}`,
	}
	for _, value := range values {
		for name, quoted := range map[string]string{"Word": Word(value), "Quote": Quote(value)} {
			out, err := exec.Command("sh", "-c", `printf '%s' `+quoted).Output()
			if err != nil {
				t.Fatalf("%s(%q) = %s: %v", name, value, quoted, err)
			}
			if string(out) != value {
				t.Fatalf("%s(%q) = %s read back as %q", name, value, quoted, out)
			}
		}
	}
}

func TestWordLeavesLiteralWordsAlone(t *testing.T) {
	for _, value := range []string{"claude", "--session-id", "/usr/bin/pi", "~/.local/bin/claude", "key=value", "ses_01"} {
		if got := Word(value); got != value {
			t.Fatalf("Word(%q) = %q", value, got)
		}
	}
}
