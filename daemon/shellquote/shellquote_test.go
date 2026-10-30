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

func TestReplaceFieldsKeepsEveryOtherByte(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		replace  map[int]string
		want     string
		ok       bool
	}{
		{"value", `env PATH='/a b':$PATH claude --mode  auto  --x "y z"`, map[int]string{4: "bypass"}, `env PATH='/a b':$PATH claude --mode  bypass  --x "y z"`, true},
		{"quoted field", `claude "--mode=auto" x`, map[int]string{1: "--mode=bypass"}, `claude --mode=bypass x`, true},
		{"needs quoting", `claude a`, map[int]string{1: "b c"}, `claude 'b c'`, true},
		{"out of range", `claude a`, map[int]string{2: "b"}, "", false},
		{"not simple", `claude a; b`, map[int]string{1: "b"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReplaceFields(tc.in, tc.replace)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ReplaceFields(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
