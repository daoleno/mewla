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

// The prompt a person or Brain reads keeps its spaces; only the key matched
// against the transcript is whitespace-free.
func TestParseClaudeChoiceScreen_QuestionTextKeepsItsSpaces(t *testing.T) {
	for name, tc := range map[string]struct{ pane, key, text string }{
		"one line": {"←  ☐ Fruit  ✔ Submit  →\nWhich fruit?\n❯ 1. Apple\n  2. Type something.\n\nEnter to select · Esc to cancel\n", "Whichfruit?", "Which fruit?"},
		"wrapped":  {"←  ☐ Fruit  ✔ Submit  →\nWhich fruit should the\n  worker pick first?\n❯ 1. Apple\n  2. Type something.\n\nEnter to select · Esc to cancel\n", "Whichfruitshouldtheworkerpickfirst?", "Which fruit should the worker pick first?"},
		"cjk":      {"←  ☐ 水果  ✔ Submit  →\n你想要哪种\n  水果？\n❯ 1. 苹果\n  2. Type something.\n\nEnter to select · Esc to cancel\n", "你想要哪种水果？", "你想要哪种水果？"},
	} {
		t.Run(name, func(t *testing.T) {
			screen := parseClaudeChoiceScreen(tc.pane)
			if screen.Kind != claudeChoiceScreenQuestion || screen.Question != tc.key || screen.QuestionText != tc.text {
				t.Fatalf("kind=%v key=%q text=%q", screen.Kind, screen.Question, screen.QuestionText)
			}
		})
	}
}

// Claude Code 2.1.318 draws a long question behind a "│ " gutter on every
// line, on the question screen and on the review screen. The gutter is not
// part of the question: with it in the key, the prompt never matched the
// transcript and every answer was refused as "the question has passed".
func TestParseClaudeChoiceScreen_LongQuestionGutterIsNotQuestionText(t *testing.T) {
	const longQuestion = "Exporting readable WeChat history needs decrypting the local database with a key from the running WeChat — there's no lighter way. The safe version is doing that openly, on your own Mac and account. How do you want to proceed?"
	screen := readClaudeChoiceFixture(t, "wide_long_question_gutter.txt")
	question := ClaudeChoiceQuestion{
		Question:    longQuestion,
		Options:     []string{"GreenBubbles on my Mac", "yichen (gentler re-sign)", "Screen-read new messages", "Hold for now"},
		AllowsOther: true,
	}
	if !isClaudeChoiceQuestion(screen, question) {
		t.Fatalf("screen = %+v", screen)
	}
	if screen.QuestionText != longQuestion {
		t.Fatalf("question text = %q", screen.QuestionText)
	}

	review := readClaudeChoiceFixture(t, "wide_long_review_gutter.txt")
	questions := []ClaudeChoiceQuestion{
		{Question: "Which export path should we take for the WeChat history, given that decrypting the local database needs a key from the running WeChat and there is no lighter way around it for this account?", Options: []string{"GreenBubbles", "Screen read"}, AllowsOther: true},
		{Question: "Which of these safeguards do you want switched on before anything touches the database, given that the sudo step and the re-signing step are both irreversible until the next update?", MultiSelect: true, Options: []string{"Backup first", "Dry run", "Audit log"}, AllowsOther: true},
	}
	if review.Kind != claudeChoiceScreenReview || !screenShowsChoice(review, questions) {
		t.Fatalf("review = %+v", review)
	}
	if !claudeChoiceAnswerMatches(review.Review[1].Answer, []string{"Backup first", "Audit log", "先问我再做"}, claudeChoiceCompact) {
		t.Fatalf("answer = %q", review.Review[1].Answer)
	}
}

// A 3-question card captured from claude 2.1.318 (the Brain's WOC question):
// long questions carry the gutter on every line, the review lists them with
// " │ ● " / " │   " and a bare "   → " answer line, and at 40 columns the
// tab bar abbreviates the headers.
var threeQuestionUserCard = []ClaudeChoiceQuestion{
	{Question: "WechatOnCloud login: the WOC WeChat still isn't logged in, and export/agent-control/human-access all need it. With a secondary account, how do you want to log in?", Options: []string{"I scan the QR", "Phone use scans it", "Hold login for now"}, AllowsOther: true},
	{Question: "Where should WechatOnCloud live, now that it's becoming a persistent hub (export + agent control + human access)?", Options: []string{"Own isolated box", "Keep on freeride-sg-app"}, AllowsOther: true},
	{Question: "This is also a test of the fake-send fix. Did answering this options card work — did it submit without switching to Terminal mode?", Options: []string{"Worked, submitted fine", "Still broken"}, AllowsOther: true},
}

func TestParseClaudeChoiceScreen_ThreeQuestionCardAcrossWidths(t *testing.T) {
	for _, name := range []string{"wide_3q_user_q1.txt", "narrow_3q_user_q1.txt"} {
		screen := readClaudeChoiceFixture(t, name)
		if !isClaudeChoiceQuestion(screen, threeQuestionUserCard[0]) || len(screen.Tabs) != 3 {
			t.Fatalf("%s: screen = %+v", name, screen)
		}
		if err := verifyPristineClaudeChoice(screen, threeQuestionUserCard); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	second := readClaudeChoiceFixture(t, "wide_3q_user_q2.txt")
	if !isClaudeChoiceQuestion(second, threeQuestionUserCard[1]) || !second.TabsAnswered[0] || second.TabsAnswered[1] {
		t.Fatalf("q2 = %+v", second)
	}
	review := readClaudeChoiceFixture(t, "wide_3q_user_review.txt")
	if review.Kind != claudeChoiceScreenReview || !screenShowsChoice(review, threeQuestionUserCard) {
		t.Fatalf("review = %+v", review)
	}
	for index, want := range []string{"I scan the QR", "Own isolated box", "Worked, submitted fine"} {
		if review.Review[index].Answer != want {
			t.Fatalf("review[%d] = %q, want %q", index, review.Review[index].Answer, want)
		}
	}
	if len(review.ReviewRows) != 2 || review.ReviewRows[0].Text != "Submit answers" || !review.ReviewRows[0].Cursor {
		t.Fatalf("review rows = %+v", review.ReviewRows)
	}
}

func TestParseClaudeChoiceScreen_ThreeQuestionExportCard(t *testing.T) {
	first := readClaudeChoiceFixture(t, "wide_3q_export_q1.txt")
	if first.Kind != claudeChoiceScreenQuestion || len(first.Tabs) != 3 || len(first.Rows) != 5 || first.cursorRow() != 1 {
		t.Fatalf("q1 = %+v", first)
	}
	review := readClaudeChoiceFixture(t, "wide_3q_export_review.txt")
	if review.Kind != claudeChoiceScreenReview || len(review.Review) != 3 {
		t.Fatalf("review = %+v", review)
	}
	if got := claudeChoiceCompact(review.Review[0].Question); got != claudeChoiceCompact("Exporting readable WeChat history needs decrypting the local database with a key taken from the running WeChat, and there is no lighter way to do it. The safe version is doing that openly, on your own Mac and account, with every step logged. How do you want to proceed with the export of the full chat history?") {
		t.Fatalf("review question = %q", got)
	}
	if review.Review[2].Question != "After the export, how should Brain summarize the groups?" || review.Review[2].Answer != "Daily digest per group" {
		t.Fatalf("review[2] = %+v", review.Review[2])
	}
}

// A card taller than the pane scrolls its tab bar away; it is still an open
// prompt and must not read as one that has passed.
func TestParseClaudeChoiceScreen_CardTallerThanThePaneIsClipped(t *testing.T) {
	for _, name := range []string{"clipped_3q_user_q1.txt", "clipped_3q_user_review.txt"} {
		if screen := readClaudeChoiceFixture(t, name); screen.Kind != claudeChoiceScreenClipped {
			t.Fatalf("%s: screen = %+v", name, screen)
		}
	}
}
