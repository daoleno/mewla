package watcher

import (
	"os"
	"path/filepath"
	"testing"
)

// Fixtures are real Claude Code 2.1.296 pane captures (capture-pane -p).
func readClaudeChoiceFixture(t *testing.T, name string) claudeChoiceScreen {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude_choice", name))
	if err != nil {
		t.Fatal(err)
	}
	return parseClaudeChoiceScreen(string(data))
}

func TestParseClaudeChoiceScreen_NarrowWrappedCJKQuestion(t *testing.T) {
	screen := readClaudeChoiceFixture(t, "narrow_cjk_q1.txt")
	question := ClaudeChoiceQuestion{
		Question:    "这个功能上线前，你希望先在哪个平台上做完整的真机验证，然后再推送给所有用户？",
		Options:     []string{"安卓", "苹果"},
		AllowsOther: true,
	}
	if !isClaudeChoiceQuestion(screen, question) {
		t.Fatalf("screen = %+v", screen)
	}
	if got := screen.Tabs; len(got) != 2 || got[0] != "平台" || got[1] != "Checks" {
		t.Fatalf("tabs = %#v", got)
	}
	if screen.cursorRow() != 1 || screen.Rows[2].Text != "Type something." {
		t.Fatalf("rows = %+v", screen.Rows)
	}
	if moved := readClaudeChoiceFixture(t, "narrow_q1_cursor2.txt"); moved.cursorRow() != 2 {
		t.Fatalf("cursor = %d", moved.cursorRow())
	}
}

func TestParseClaudeChoiceScreen_MultiSelectStates(t *testing.T) {
	question := ClaudeChoiceQuestion{
		Question:    "Which checks?",
		MultiSelect: true,
		Options:     []string{"Lint", "Tests", "Build"},
		AllowsOther: true,
	}
	empty := readClaudeChoiceFixture(t, "narrow_q2_multi_empty.txt")
	if !isClaudeChoiceQuestion(empty, question) || empty.TabsAnswered[0] != true || empty.TabsAnswered[1] {
		t.Fatalf("empty = %+v", empty)
	}
	for _, row := range empty.Rows {
		if !row.HasBox || row.Checked {
			t.Fatalf("empty row = %+v", row)
		}
	}
	tests := readClaudeChoiceFixture(t, "narrow_q2_multi_tests.txt")
	if !tests.Rows[1].Checked || !tests.Rows[1].Cursor || tests.Rows[0].Checked {
		t.Fatalf("tests rows = %+v", tests.Rows)
	}
	other := readClaudeChoiceFixture(t, "narrow_q2_multi_other.txt")
	row := other.Rows[3]
	if !row.Cursor || !row.Checked || row.Full != `-x; 中文 "q"` {
		t.Fatalf("other row = %+v", row)
	}
	submit := readClaudeChoiceFixture(t, "narrow_q2_submit_row.txt")
	if !submit.SubmitRow || !submit.SubmitCursor || submit.cursorRow() != 0 {
		t.Fatalf("submit = %+v", submit)
	}
}

func TestParseClaudeChoiceScreen_PreviewLayoutHasNoOtherRow(t *testing.T) {
	question := ClaudeChoiceQuestion{Question: "Pick a layout?", Options: []string{"Grid", "List"}}
	screen := readClaudeChoiceFixture(t, "wide_preview_q1.txt")
	if !isClaudeChoiceQuestion(screen, question) {
		t.Fatalf("screen = %+v", screen)
	}
	if screen.Rows[0].Text != "Grid" || screen.Rows[1].Text != "List" {
		t.Fatalf("rows = %+v", screen.Rows)
	}
	if moved := readClaudeChoiceFixture(t, "wide_preview_q1_cursor2.txt"); moved.cursorRow() != 2 {
		t.Fatalf("cursor = %d", moved.cursorRow())
	}
	if isClaudeChoiceQuestion(screen, ClaudeChoiceQuestion{Question: "Pick a layout?", Options: []string{"Grid", "List"}, AllowsOther: true}) {
		t.Fatal("a preview question must not be proven with a free-text row")
	}
}

func TestParseClaudeChoiceScreen_ReviewScreens(t *testing.T) {
	narrow := readClaudeChoiceFixture(t, "narrow_review.txt")
	if narrow.Kind != claudeChoiceScreenReview || len(narrow.Review) != 2 {
		t.Fatalf("narrow = %+v", narrow)
	}
	if got := claudeChoiceCompact(narrow.Review[0].Question); got != claudeChoiceCompact("这个功能上线前，你希望先在哪个平台上做完整的真机验证，然后再推送给所有用户？") {
		t.Fatalf("question = %q", got)
	}
	if !claudeChoiceAnswerMatches(narrow.Review[1].Answer, []string{"Tests", `-x; 中文 "q" endA;;B\\ A;中-;`}, claudeChoiceCompact) {
		t.Fatalf("answer = %q", narrow.Review[1].Answer)
	}
	if narrow.ReviewRows[0].Text != "Submit answers" || !narrow.ReviewRows[0].Cursor || narrow.ReviewRows[1].Text != "Cancel" {
		t.Fatalf("review rows = %+v", narrow.ReviewRows)
	}
	single := readClaudeChoiceFixture(t, "wide_single_multi_after_submit.txt")
	if single.Kind != claudeChoiceScreenReview || single.Review[0].Answer != "Large" || len(single.Tabs) != 1 {
		t.Fatalf("single = %+v", single)
	}
}

func TestParseClaudeChoiceScreen_OtherPromptsAreNotChoices(t *testing.T) {
	for _, name := range []string{"permission_bash.txt", "exitplanmode.txt"} {
		if screen := readClaudeChoiceFixture(t, name); screen.Kind != claudeChoiceScreenNone {
			t.Fatalf("%s parsed as %+v", name, screen)
		}
	}
}

func TestClaudeChoiceAnswerMatches(t *testing.T) {
	cases := []struct {
		recorded string
		parts    []string
		want     bool
	}{
		{"Banana", []string{"Banana"}, true},
		{"Red, Blue, Mauve 紫", []string{"Red", "Blue", "Mauve 紫"}, true},
		{`Tests, "-x; 中文 \"q\" endA;;B\\\\ A;中-;"`, []string{"Tests", `-x; 中文 "q" endA;;B\\ A;中-;`}, true},
		{"Red, Blue", []string{"Red"}, false},
		{"Blue, Red", []string{"Red", "Blue"}, false},
	}
	for _, tc := range cases {
		if got := ClaudeChoiceAnswerMatches(tc.recorded, tc.parts); got != tc.want {
			t.Fatalf("%q vs %#v = %v", tc.recorded, tc.parts, got)
		}
	}
}
