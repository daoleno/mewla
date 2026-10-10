package watcher

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeClaudeChoiceTUI models the AskUserQuestion keyboard contract observed
// in Claude Code 2.1.296 and renders the same plain-text layout.
type fakeClaudeChoiceTUI struct {
	questions []ClaudeChoiceQuestion
	tab       int // len(questions) means the review screen
	cursor    int // 1-based; multi-select Submit is len(rows)+1
	checked   []map[int]bool
	other     []string
	answers   []string
	done      bool
	keys      []string
	stray     []string
	// toggleOffset simulates a desynced TUI that toggles the wrong row.
	toggleOffset int
	// renameQuestion swaps the question text after this many keys.
	renameAfter int
}

func newFakeClaudeChoiceTUI(questions ...ClaudeChoiceQuestion) *fakeClaudeChoiceTUI {
	tui := &fakeClaudeChoiceTUI{questions: questions, cursor: 1, answers: make([]string, len(questions)), other: make([]string, len(questions))}
	for range questions {
		tui.checked = append(tui.checked, map[int]bool{})
	}
	return tui
}

func (t *fakeClaudeChoiceTUI) rows(q ClaudeChoiceQuestion) int {
	if q.AllowsOther {
		return len(q.Options) + 1
	}
	return len(q.Options)
}

func (t *fakeClaudeChoiceTUI) needsReview() bool {
	return len(t.questions) > 1 || t.questions[0].MultiSelect
}

func (t *fakeClaudeChoiceTUI) capture() (string, error) {
	var b strings.Builder
	b.WriteString("● earlier output\n")
	if t.done {
		b.WriteString("● User answered Claude's questions:\n❯ \n")
		return b.String(), nil
	}
	b.WriteString(strings.Repeat("─", 60) + "\n")
	var tabs []string
	for index, q := range t.questions {
		mark := "☐"
		if t.answers[index] != "" {
			mark = "☒"
		}
		tabs = append(tabs, mark+" "+q.Question[:min(6, len(q.Question))])
	}
	if t.needsReview() {
		b.WriteString("←  " + strings.Join(tabs, "  ") + "  ✔ Submit  →\n")
	} else {
		b.WriteString(" " + tabs[0] + "\n")
	}
	if t.tab == len(t.questions) {
		b.WriteString("\nReview your answers\n\n")
		for index, q := range t.questions {
			b.WriteString(" ● " + q.Question + "\n   → " + t.answers[index] + "\n")
		}
		b.WriteString("\nReady to submit your answers?\n\n")
		b.WriteString(cursorPrefix(t.cursor == 1) + "1. Submit answers\n" + cursorPrefix(t.cursor == 2) + "2. Cancel\n")
		return b.String(), nil
	}
	q := t.questions[t.tab]
	question := q.Question
	if t.renameAfter > 0 && len(t.keys) >= t.renameAfter {
		question = "A different question?"
	}
	b.WriteString("\n" + question + "\n\n")
	for number := 1; number <= t.rows(q); number++ {
		label := "Type something."
		if number <= len(q.Options) {
			label = q.Options[number-1]
		} else if t.other[t.tab] != "" {
			label = t.other[t.tab]
		} else if q.MultiSelect {
			label = "Type something"
		}
		if q.MultiSelect {
			box := "[ ] "
			if t.checked[t.tab][number] {
				box = "[✔] "
			}
			label = box + label
		}
		fmt.Fprintf(&b, "%s%d. %s\n", cursorPrefix(t.cursor == number), number, label)
		if number <= len(q.Options) {
			b.WriteString("     description\n")
		}
	}
	if q.MultiSelect {
		b.WriteString(cursorPrefix(t.cursor == t.rows(q)+1) + "   Submit\n")
	}
	b.WriteString(strings.Repeat("─", 60) + "\n")
	fmt.Fprintf(&b, "  %d. Chat about this\n\nEnter to select · ↑/↓ to navigate · Esc to cancel\n", t.rows(q)+1)
	return b.String(), nil
}

func cursorPrefix(on bool) string {
	if on {
		return "❯ "
	}
	return "  "
}

func (t *fakeClaudeChoiceTUI) sendKey(key string) error {
	t.keys = append(t.keys, key)
	if t.done {
		t.stray = append(t.stray, key)
		return nil
	}
	if t.tab == len(t.questions) {
		switch key {
		case "Down":
			t.cursor = 2
		case "Up":
			t.cursor = 1
		case "Enter":
			if t.cursor == 1 {
				t.done = true
			}
		}
		return nil
	}
	q := t.questions[t.tab]
	last := t.rows(q)
	if q.MultiSelect {
		last++
	}
	switch key {
	case "Down":
		t.cursor = min(t.cursor+1, last)
	case "Up":
		t.cursor = max(t.cursor-1, 1)
	case "Enter":
		switch {
		case !q.MultiSelect:
			if t.cursor <= len(q.Options) {
				t.answers[t.tab] = q.Options[t.cursor-1]
			} else if t.other[t.tab] != "" {
				t.answers[t.tab] = t.other[t.tab]
			} else {
				return nil
			}
			t.advance()
		case t.cursor == last:
			var parts []string
			for number := 1; number <= len(q.Options); number++ {
				if t.checked[t.tab][number] {
					parts = append(parts, q.Options[number-1])
				}
			}
			if t.checked[t.tab][len(q.Options)+1] && t.other[t.tab] != "" {
				parts = append(parts, t.other[t.tab])
			}
			t.answers[t.tab] = strings.Join(parts, ", ")
			t.advance()
		default:
			row := t.cursor + t.toggleOffset
			t.checked[t.tab][row] = !t.checked[t.tab][row]
		}
	default:
		t.stray = append(t.stray, key)
	}
	return nil
}

func (t *fakeClaudeChoiceTUI) advance() {
	t.tab++
	t.cursor = 1
	if t.tab == len(t.questions) && !t.needsReview() {
		t.done = true
	}
}

func (t *fakeClaudeChoiceTUI) sendText(text string) error {
	t.keys = append(t.keys, "text:"+text)
	q := t.questions[t.tab]
	if t.tab == len(t.questions) || !q.AllowsOther || t.cursor != len(q.Options)+1 {
		t.stray = append(t.stray, text)
		return nil
	}
	t.other[t.tab] += text
	t.checked[t.tab][t.cursor] = true
	return nil
}

func driveFake(t *testing.T, tui *fakeClaudeChoiceTUI, answers ...ClaudeChoiceAnswer) error {
	t.Helper()
	expected, err := ExpectedClaudeChoiceAnswers(tui.questions, answers)
	if err != nil {
		return err
	}
	return driveClaudeChoice(tui, tui.questions, answers, expected, 200*time.Millisecond)
}

var (
	fruitQuestion  = ClaudeChoiceQuestion{Question: "Which fruit?", Options: []string{"Apple", "Banana", "Cherry"}, AllowsOther: true}
	colorsQuestion = ClaudeChoiceQuestion{Question: "Which colors?", MultiSelect: true, Options: []string{"Red", "Green", "Blue"}, AllowsOther: true}
)

func TestDriveClaudeChoice_TwoQuestionsWithMultiSelectAndOther(t *testing.T) {
	tui := newFakeClaudeChoiceTUI(fruitQuestion, colorsQuestion)
	err := driveFake(t, tui,
		ClaudeChoiceAnswer{Selected: []int{1}},
		ClaudeChoiceAnswer{Selected: []int{2, 0}, Other: `Mauve; "紫" -x;`},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !tui.done || len(tui.stray) != 0 {
		t.Fatalf("done=%v stray=%#v keys=%#v", tui.done, tui.stray, tui.keys)
	}
	want := []string{"Banana", `Red, Blue, Mauve; "紫" -x;`}
	if !reflect.DeepEqual(tui.answers, want) {
		t.Fatalf("answers = %#v", tui.answers)
	}
}

func TestDriveClaudeChoice_SingleQuestionOtherSubmitsWithoutReview(t *testing.T) {
	tui := newFakeClaudeChoiceTUI(ClaudeChoiceQuestion{Question: "Pick a size?", Options: []string{"Small", "Large"}, AllowsOther: true})
	if err := driveFake(t, tui, ClaudeChoiceAnswer{Other: `Medium "M" 中`}); err != nil {
		t.Fatal(err)
	}
	if !tui.done || tui.answers[0] != `Medium "M" 中` || len(tui.stray) != 0 {
		t.Fatalf("answers=%#v stray=%#v", tui.answers, tui.stray)
	}
}

func TestDriveClaudeChoice_PreviewQuestionUsesArrowsOnly(t *testing.T) {
	tui := newFakeClaudeChoiceTUI(
		ClaudeChoiceQuestion{Question: "Pick a layout?", Options: []string{"Grid", "List"}},
		ClaudeChoiceQuestion{Question: "Ship it?", Options: []string{"Yes", "No"}, AllowsOther: true},
	)
	if err := driveFake(t, tui, ClaudeChoiceAnswer{Selected: []int{1}}, ClaudeChoiceAnswer{Selected: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tui.answers, []string{"List", "Yes"}) {
		t.Fatalf("answers = %#v", tui.answers)
	}
	for _, key := range tui.keys {
		if key != "Up" && key != "Down" && key != "Enter" {
			t.Fatalf("unexpected key %q in %#v", key, tui.keys)
		}
	}
}

func TestDriveClaudeChoice_RefusesBeforeAnyKey(t *testing.T) {
	t.Run("question mismatch", func(t *testing.T) {
		tui := newFakeClaudeChoiceTUI(colorsQuestion)
		questions := []ClaudeChoiceQuestion{fruitQuestion}
		answers := []ClaudeChoiceAnswer{{Selected: []int{0}}}
		expected, _ := ExpectedClaudeChoiceAnswers(questions, answers)
		err := driveClaudeChoice(tui, questions, answers, expected, 100*time.Millisecond)
		if !errors.Is(err, ErrClaudeChoiceRefused) || len(tui.keys) != 0 {
			t.Fatalf("err=%v keys=%#v", err, tui.keys)
		}
	})
	t.Run("partly answered in Terminal", func(t *testing.T) {
		tui := newFakeClaudeChoiceTUI(colorsQuestion)
		tui.checked[0][1] = true
		err := driveFake(t, tui, ClaudeChoiceAnswer{Selected: []int{0}})
		if !errors.Is(err, ErrClaudeChoiceRefused) || len(tui.keys) != 0 {
			t.Fatalf("err=%v keys=%#v", err, tui.keys)
		}
	})
	t.Run("invalid answers", func(t *testing.T) {
		for _, answer := range []ClaudeChoiceAnswer{
			{},
			{Selected: []int{0, 1}},
			{Selected: []int{9}},
			{Other: "two\nlines"},
			{Other: " padded"},
		} {
			if _, err := ExpectedClaudeChoiceAnswers([]ClaudeChoiceQuestion{fruitQuestion}, []ClaudeChoiceAnswer{answer}); !errors.Is(err, ErrClaudeChoiceRefused) {
				t.Fatalf("answer %+v accepted", answer)
			}
		}
		preview := ClaudeChoiceQuestion{Question: "Pick?", Options: []string{"A", "B"}}
		if _, err := ExpectedClaudeChoiceAnswers([]ClaudeChoiceQuestion{preview}, []ClaudeChoiceAnswer{{Other: "x"}}); !errors.Is(err, ErrClaudeChoiceRefused) {
			t.Fatal("other text accepted for a preview question")
		}
	})
}

func TestDriveClaudeChoice_StopsWithoutSubmittingWhenTheScreenDisagrees(t *testing.T) {
	t.Run("wrong row toggles", func(t *testing.T) {
		tui := newFakeClaudeChoiceTUI(fruitQuestion, colorsQuestion)
		tui.toggleOffset = 1
		err := driveFake(t, tui, ClaudeChoiceAnswer{Selected: []int{0}}, ClaudeChoiceAnswer{Selected: []int{0}})
		if !errors.Is(err, ErrClaudeChoiceRefused) || tui.done || tui.tab == len(tui.questions) {
			t.Fatalf("err=%v done=%v tab=%d", err, tui.done, tui.tab)
		}
	})
	t.Run("question changes mid-flight", func(t *testing.T) {
		tui := newFakeClaudeChoiceTUI(fruitQuestion)
		tui.renameAfter = 1
		err := driveFake(t, tui, ClaudeChoiceAnswer{Selected: []int{2}})
		if !errors.Is(err, ErrClaudeChoiceRefused) || tui.done || tui.answers[0] != "" {
			t.Fatalf("err=%v done=%v answers=%#v", err, tui.done, tui.answers)
		}
		if !strings.Contains(err.Error(), "still open in the Terminal") {
			t.Fatalf("err = %v", err)
		}
	})
}
