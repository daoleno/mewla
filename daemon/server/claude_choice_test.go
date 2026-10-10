package server

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

func choiceConversation(state string, answers ...string) work.CodexConversation {
	return work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{{
		ID:       "claude-tool:toolu_1",
		Kind:     "tool",
		ToolName: work.ClaudeAskUserQuestionTool,
		CallID:   "toolu_1",
		Choice: &work.ConversationChoice{
			Kind:  work.ClaudeAskUserQuestionTool,
			State: state,
			Questions: []work.ConversationChoiceQuestion{
				{Question: "Which fruit?", Options: []work.ConversationChoiceOption{{Label: "Apple"}, {Label: "Banana"}}},
				{Question: "Which colors?", MultiSelect: true, Options: []work.ConversationChoiceOption{{Label: "Red"}, {Label: "Blue"}}},
			},
			Answers: answers,
		},
	}}}
}

func answerRequest(callID string) answerChoiceRequest {
	request := answerChoiceRequest{CallID: callID}
	request.Answers = append(request.Answers, struct {
		Selected []int  `json:"selected"`
		Other    string `json:"other"`
	}{Selected: []int{1}})
	request.Answers = append(request.Answers, struct {
		Selected []int  `json:"selected"`
		Other    string `json:"other"`
	}{Selected: []int{0}, Other: "Teal"})
	return request
}

func TestAnswerChoice_DrivesTranscriptQuestionsAndConfirmsRecordedAnswers(t *testing.T) {
	var drove []watcher.ClaudeChoiceQuestion
	var droveAnswers []watcher.ClaudeChoiceAnswer
	srv := &Server{answerClaudeChoiceOverride: func(workerID string, questions []watcher.ClaudeChoiceQuestion, answers []watcher.ClaudeChoiceAnswer) error {
		drove, droveAnswers = questions, answers
		return nil
	}}
	loads := 0
	load := func() (work.CodexConversation, error) {
		loads++
		if loads == 1 {
			return choiceConversation(work.ConversationChoicePending), nil
		}
		return choiceConversation(work.ConversationChoiceAnswered, "Banana", "Red, Teal"), nil
	}
	got, err := srv.answerChoice("%7", answerRequest("toolu_1"), load)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"Banana", "Red, Teal"}) {
		t.Fatalf("answers = %#v", got)
	}
	want := []watcher.ClaudeChoiceQuestion{
		{Question: "Which fruit?", Options: []string{"Apple", "Banana"}, AllowsOther: true},
		{Question: "Which colors?", MultiSelect: true, Options: []string{"Red", "Blue"}, AllowsOther: true},
	}
	if !reflect.DeepEqual(drove, want) || droveAnswers[1].Other != "Teal" {
		t.Fatalf("drove %#v %#v", drove, droveAnswers)
	}
}

func TestAnswerChoice_RefusesStaleCallsAndReportsMismatches(t *testing.T) {
	driven := false
	srv := &Server{answerClaudeChoiceOverride: func(string, []watcher.ClaudeChoiceQuestion, []watcher.ClaudeChoiceAnswer) error {
		driven = true
		return nil
	}}
	answered := func() (work.CodexConversation, error) {
		return choiceConversation(work.ConversationChoiceAnswered, "Apple", "Red"), nil
	}
	if _, err := srv.answerChoice("%7", answerRequest("toolu_1"), answered); !errors.Is(err, watcher.ErrClaudeChoiceRefused) || driven {
		t.Fatalf("answered choice: err=%v driven=%v", err, driven)
	}
	pending := func() (work.CodexConversation, error) {
		return choiceConversation(work.ConversationChoicePending), nil
	}
	if _, err := srv.answerChoice("%7", answerRequest("toolu_other"), pending); !errors.Is(err, watcher.ErrClaudeChoiceRefused) || driven {
		t.Fatalf("unknown call: err=%v driven=%v", err, driven)
	}

	loads := 0
	mismatch := func() (work.CodexConversation, error) {
		loads++
		if loads == 1 {
			return choiceConversation(work.ConversationChoicePending), nil
		}
		return choiceConversation(work.ConversationChoiceAnswered, "Apple", "Red, Teal"), nil
	}
	_, err := srv.answerChoice("%7", answerRequest("toolu_1"), mismatch)
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("mismatch err = %v", err)
	}
}
