package work

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The fixture is a trimmed real Claude Code 2.1.296 transcript: four
// AskUserQuestion calls answered in the TUI (plain list, multi-select with
// typed Other text, preview layout, single question with Other).
func TestParseClaudeConversation_ProjectsAskUserQuestionChoices(t *testing.T) {
	got, err := parseClaudeConversation(filepath.Join("testdata", "claude_ask_user_question.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var choices []CodexConversationEvent
	for _, event := range got.Events {
		if event.Choice != nil {
			choices = append(choices, event)
		}
	}
	if len(choices) != 4 {
		t.Fatalf("choice events = %d, want 4", len(choices))
	}

	first := choices[0]
	if first.ToolName != ClaudeAskUserQuestionTool || first.CallID != "toolu_01E81v6H52QVbKoHZsGvJDfJ" {
		t.Fatalf("first choice identity = %q %q", first.ToolName, first.CallID)
	}
	if first.Choice.State != ConversationChoiceAnswered {
		t.Fatalf("first state = %q", first.Choice.State)
	}
	if len(first.Choice.Questions) != 2 ||
		first.Choice.Questions[0].Question != "Which fruit?" ||
		first.Choice.Questions[0].MultiSelect ||
		!first.Choice.Questions[1].MultiSelect ||
		first.Choice.Questions[1].Options[2].Label != "Blue" ||
		first.Choice.Questions[1].Options[2].Description != "cool" {
		t.Fatalf("first questions = %+v", first.Choice.Questions)
	}
	if want := []string{"Banana", "Red, Blue, Mauve 紫"}; !reflect.DeepEqual(first.Choice.Answers, want) {
		t.Fatalf("first answers = %#v, want %#v", first.Choice.Answers, want)
	}

	preview := choices[1]
	if !preview.Choice.Questions[0].HasPreview() || preview.Choice.Questions[0].AllowsOther() {
		t.Fatalf("preview question = %+v", preview.Choice.Questions[0])
	}
	if want := []string{"Dog", "Hammer, Drill"}; !reflect.DeepEqual(preview.Choice.Answers, want) {
		t.Fatalf("preview answers = %#v", preview.Choice.Answers)
	}

	single := choices[2]
	if want := []string{`Medium "M" 中`}; !reflect.DeepEqual(single.Choice.Answers, want) {
		t.Fatalf("single answers = %#v", single.Choice.Answers)
	}

	quoted := choices[3]
	if want := []string{"苹果", `Tests, "-x; 中文 \"q\" endA;;B\\\\ A;中-;"`}; !reflect.DeepEqual(quoted.Choice.Answers, want) {
		t.Fatalf("quoted answers = %#v", quoted.Choice.Answers)
	}
}

func TestParseClaudeConversation_PendingAndDeclinedChoices(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	longPreview := strings.Repeat("界", 9000)
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-10-10T15:00:00Z","message":{"role":"user","content":"ask me"}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s","timestamp":"2026-10-10T15:00:01Z","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_old","name":"AskUserQuestion","input":{"questions":[{"question":"Old?","header":"Old","multiSelect":false,"options":[{"label":"A","description":"a"},{"label":"B","description":"b"}]}]}}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"s","timestamp":"2026-10-10T15:00:02Z","toolUseResult":"User rejected tool use","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_old","is_error":true,"content":"The user doesn't want to proceed with this tool use."}]}}`,
		`{"type":"assistant","uuid":"a2","sessionId":"s","timestamp":"2026-10-10T15:00:03Z","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_new","name":"AskUserQuestion","input":{"questions":[{"question":"New?","header":"New","multiSelect":true,"options":[{"label":"X","description":"x","preview":"` + longPreview + `"},{"label":"Y","description":"y"}]}]}}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	var old, pending *CodexConversationEvent
	for index := range got.Events {
		switch got.Events[index].CallID {
		case "toolu_old":
			old = &got.Events[index]
		case "toolu_new":
			pending = &got.Events[index]
		}
	}
	if old == nil || old.Choice == nil || old.Choice.State != ConversationChoiceDeclined || old.Choice.Answers != nil {
		t.Fatalf("old choice = %+v", old)
	}
	if pending == nil || pending.Choice == nil || pending.Choice.State != ConversationChoicePending {
		t.Fatalf("pending choice = %+v", pending)
	}
	// The raw tool input is truncated for display, but the choice still parses.
	if len([]rune(pending.Input)) > maxCodexConversationBody {
		t.Fatalf("input not truncated: %d runes", len([]rune(pending.Input)))
	}
	if got := len([]rune(pending.Choice.Questions[0].Options[0].Preview)); got != maxConversationChoicePreview {
		t.Fatalf("preview runes = %d", got)
	}

	if _, ok := PendingConversationChoice(got, "toolu_new"); !ok {
		t.Fatal("latest pending choice was not found")
	}
	if _, ok := PendingConversationChoice(got, "toolu_old"); ok {
		t.Fatal("declined older choice must not be pending")
	}
}

func TestPendingConversationChoice_OnlyNewestChoiceIsLive(t *testing.T) {
	pending := &ConversationChoice{Kind: ClaudeAskUserQuestionTool, State: ConversationChoicePending}
	conversation := CodexConversation{Events: []CodexConversationEvent{
		{ID: "1", CallID: "stale", Choice: pending},
		{ID: "2", Kind: "assistant_message", Body: "moved on"},
		{ID: "3", CallID: "live", Choice: pending},
	}}
	if _, ok := PendingConversationChoice(conversation, "stale"); ok {
		t.Fatal("an older unmatched choice must not be live")
	}
	if event, ok := PendingConversationChoice(conversation, "live"); !ok || event.ID != "3" {
		t.Fatalf("live = %+v %v", event, ok)
	}
}

func TestParseClaudeConversation_ChoiceAbandonedWhenConversationMovesOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-10-10T15:00:00Z","message":{"role":"user","content":"ask me"}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s","timestamp":"2026-10-10T15:00:01Z","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_lost","name":"AskUserQuestion","input":{"questions":[{"question":"Lost?","options":[{"label":"A"},{"label":"B"}]}]}}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"s","timestamp":"2026-10-10T15:05:00Z","message":{"role":"user","content":"never mind, do something else"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range got.Events {
		if event.CallID == "toolu_lost" {
			if event.Choice == nil || event.Choice.State != ConversationChoiceUnanswered {
				t.Fatalf("choice = %+v", event.Choice)
			}
			if _, ok := PendingConversationChoice(got, "toolu_lost"); ok {
				t.Fatal("an abandoned choice must not be answerable")
			}
			return
		}
	}
	t.Fatal("choice event missing")
}
