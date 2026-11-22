package agysession

import "testing"

func TestIDIsReadOnlyFromMewlasOwnCommand(t *testing.T) {
	const id = "agy-0b6a3c1e-0000-4000-8000-00000000abcd"
	for command, want := range map[string]string{
		"/opt/mewla agy-session --agy-session " + id + " --state-dir /s --model m": id,
		"mewla agy-session --agy-session=" + id:                                    id,
		"/opt/mewla-agy6 agy-session --agy-session " + id:                          id,
		"agy agy-session --agy-session " + id:                                      "",
		"/opt/mewla agy-session --agy-session agy-short":                           "",
		"/opt/mewla dsh-session --dsh-session " + id:                               "",
		"agy --conversation " + id:                                                 "",
	} {
		if got := ID(command); got != want {
			t.Errorf("ID(%q) = %q, want %q", command, got, want)
		}
	}
	if Option("/opt/mewla agy-session --agy-session "+id+" --conversation c1 --model=gemini-4-neon-low", "--model") != "gemini-4-neon-low" {
		t.Fatal("--model=value is read")
	}
	if Option("/opt/mewla agy-session --conversation c1", "--conversation") != "c1" {
		t.Fatal("--conversation value is read")
	}
}

func TestScreenReadsTheSessionsOwnTerminal(t *testing.T) {
	header := "Antigravity · Gemini 4 Neon (Low) · /work"
	cases := []struct {
		name           string
		lines          []string
		ready, blocked bool
		summary        string
	}{
		{"composer waits", []string{header, "", "› hi", "  hello", "", "›"}, true, false, "hello"},
		{"typed draft", []string{header, "  hello", "› draft"}, true, false, "hello"},
		{"permission", []string{header, "  ● echo hi", "", "Allow Command · echo hi?", "  1. Allow", "  2. Deny", "?"}, true, true, "Allow Command · echo hi?"},
		{"question", []string{header, "", "Asked: Which fruit?", "  1. Apple", "  2. Pear", "? "}, true, true, "Which fruit?"},
		{"working", []string{header, "› hi", "  · Thinking"}, false, false, "· Thinking"},
		{"not the program", []string{"$ ls", "›"}, false, false, "$ ls"},
		{"empty", nil, false, false, ""},
	}
	for _, c := range cases {
		ready, blocked, summary := Screen(c.lines)
		if ready != c.ready || blocked != c.blocked || summary != c.summary {
			t.Errorf("%s: Screen = %v %v %q, want %v %v %q", c.name, ready, blocked, summary, c.ready, c.blocked, c.summary)
		}
	}
}
