package server

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

type fakePanePrompts struct {
	current    *classifier.Prompt
	currentErr error
	pressed    []int
	pressErr   error
	drove      []watcher.ClaudeChoiceAnswer
	driveErr   error
}

func (f *fakePanePrompts) CurrentPrompt(string) (*classifier.Prompt, error) {
	return f.current, f.currentErr
}

func (f *fakePanePrompts) AnswerPrompt(_, _ string, number int) error {
	f.pressed = append(f.pressed, number)
	return f.pressErr
}

func (f *fakePanePrompts) AnswerClaudeChoice(_ string, _ []watcher.ClaudeChoiceQuestion, answers []watcher.ClaudeChoiceAnswer) error {
	f.drove = answers
	return f.driveErr
}

func permissionPrompt() *classifier.Prompt {
	return &classifier.Prompt{ID: "perm1", Kind: classifier.PromptPermission, Options: []classifier.PromptOption{
		{Number: 1, Label: "Yes"},
		{Number: 2, Label: "Yes, and don't ask again for this command"},
		{Number: 3, Label: "No, and tell Claude what to do differently"},
	}}
}

func TestAnswerPanePromptMenusAndStalePrompts(t *testing.T) {
	loads := func() (work.CodexConversation, error) { return work.CodexConversation{}, nil }
	cases := []struct {
		name        string
		pane        *fakePanePrompts
		answer      PromptAnswer
		hook        PromptHook
		wantPressed []int
		wantGone    bool
		wantErr     bool
	}{
		{name: "menu option is pressed", pane: &fakePanePrompts{current: permissionPrompt()}, answer: PromptAnswer{PromptID: "perm1", Number: 2}, wantPressed: []int{2}},
		{name: "a different prompt id sends no key", pane: &fakePanePrompts{current: permissionPrompt()}, answer: PromptAnswer{PromptID: "old", Number: 1}, wantGone: true},
		{name: "no prompt open sends no key", pane: &fakePanePrompts{}, answer: PromptAnswer{PromptID: "perm1", Number: 1}, wantGone: true},
		{name: "missing prompt id", pane: &fakePanePrompts{current: permissionPrompt()}, answer: PromptAnswer{Number: 1}, wantErr: true},
		{name: "pane read failure", pane: &fakePanePrompts{currentErr: errors.New("capture pane")}, answer: PromptAnswer{PromptID: "perm1", Number: 1}, wantErr: true},
		{name: "hook takes the answer", pane: &fakePanePrompts{current: permissionPrompt()}, answer: PromptAnswer{PromptID: "perm1", Number: 1},
			hook: func(classifier.Prompt, int) (bool, error) { return true, nil }},
		{name: "hook refusal sends no key", pane: &fakePanePrompts{current: permissionPrompt()}, answer: PromptAnswer{PromptID: "perm1", Number: 1},
			hook: func(classifier.Prompt, int) (bool, error) {
				return false, &ApprovalDecisionError{Code: "approval_basis_required", Message: "needs basis"}
			}, wantErr: true},
		{name: "unhandled hook falls to the pane", pane: &fakePanePrompts{current: permissionPrompt()}, answer: PromptAnswer{PromptID: "perm1", Number: 3},
			hook: func(classifier.Prompt, int) (bool, error) { return false, nil }, wantPressed: []int{3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AnswerPanePrompt(tc.pane, "%1", tc.answer, tc.hook, loads)
			if (err != nil) != (tc.wantErr || tc.wantGone) {
				t.Fatalf("err = %v", err)
			}
			if tc.wantGone && !PromptGone(err) {
				t.Fatalf("err = %v, want the prompt has passed", err)
			}
			if !reflect.DeepEqual(tc.pane.pressed, tc.wantPressed) {
				t.Fatalf("pressed %v, want %v", tc.pane.pressed, tc.wantPressed)
			}
		})
	}
}

func TestAnswerPanePromptChoiceUsesTranscriptQuestions(t *testing.T) {
	prompt := &classifier.Prompt{ID: "choice1", Kind: classifier.PromptChoice}
	pane := &fakePanePrompts{current: prompt}
	loads := 0
	load := func() (work.CodexConversation, error) {
		loads++
		if loads <= 2 {
			return choiceConversation(work.ConversationChoicePending), nil
		}
		return choiceConversation(work.ConversationChoiceAnswered, "Banana", "Red, Teal"), nil
	}
	answer := PromptAnswer{PromptID: "choice1", Answers: []ChoiceSelection{{Selected: []int{1}}, {Selected: []int{0}, Other: "Teal"}}}
	got, err := AnswerPanePrompt(pane, "%1", answer, nil, load)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"Banana", "Red, Teal"}) || len(pane.drove) != 2 || pane.drove[1].Other != "Teal" || len(pane.pressed) != 0 {
		t.Fatalf("recorded %v drove %+v pressed %v", got, pane.drove, pane.pressed)
	}

	cases := []struct {
		name     string
		prompt   *classifier.Prompt
		answer   PromptAnswer
		conv     work.CodexConversation
		wantGone bool
	}{
		{name: "stale prompt id", prompt: prompt, answer: PromptAnswer{PromptID: "choice0", Answers: answer.Answers}, conv: choiceConversation(work.ConversationChoicePending), wantGone: true},
		{name: "transcript has no pending question", prompt: prompt, answer: answer, conv: choiceConversation(work.ConversationChoiceAnswered, "Apple", "Red"), wantGone: true},
		{name: "a menu number does not answer a question", prompt: prompt, answer: PromptAnswer{PromptID: "choice1", Number: 1}, conv: choiceConversation(work.ConversationChoicePending)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := &fakePanePrompts{current: tc.prompt}
			_, err := AnswerPanePrompt(pane, "%1", tc.answer, nil, func() (work.CodexConversation, error) { return tc.conv, nil })
			if err == nil || PromptGone(err) != tc.wantGone || pane.drove != nil || len(pane.pressed) != 0 {
				t.Fatalf("err=%v gone=%v drove=%v pressed=%v", err, PromptGone(err), pane.drove, pane.pressed)
			}
		})
	}
}

func TestApprovalOptionForPrompt(t *testing.T) {
	prompt := *permissionPrompt()
	cases := []struct {
		number int
		kind   classifier.PromptKind
		want   string
		ok     bool
	}{
		{1, classifier.PromptPermission, "allow", true},
		{2, classifier.PromptPermission, "", false},
		{3, classifier.PromptPermission, "deny", true},
		{9, classifier.PromptPermission, "", false},
		{1, classifier.PromptPlan, "", false},
	}
	for _, tc := range cases {
		prompt.Kind = tc.kind
		got, ok := ApprovalOptionForPrompt(prompt, tc.number)
		if got != tc.want || ok != tc.ok {
			t.Errorf("option %d kind %s = %q %v", tc.number, tc.kind, got, ok)
		}
	}
}

func TestPromptAnswerCode(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("%w: %w", watcher.ErrPromptGone, watcher.ErrClaudeChoiceRefused), "answer_prompt_gone"},
		{watcher.ErrClaudeChoiceGone, "answer_prompt_gone"},
		{fmt.Errorf("%w: the prompt has no option 7", watcher.ErrClaudeChoiceRefused), "answer_prompt_refused"},
		{&ApprovalDecisionError{Code: "approval_with_brain", Message: "Brain is handling this"}, "answer_prompt_refused"},
		{errors.New("tmux died"), "answer_prompt_failed"},
	}
	for _, tc := range cases {
		if code, message := PromptAnswerCode(tc.err); code != tc.code || message == "" {
			t.Errorf("%v = %q %q, want %q", tc.err, code, message, tc.code)
		}
	}
}

type recordingApprovals struct {
	pending []ApprovalWire
	decided []string
	err     error
}

func (r *recordingApprovals) Pending(string) []ApprovalWire { return r.pending }
func (r *recordingApprovals) Decide(_, id, option string) error {
	r.decided = append(r.decided, id+":"+option)
	return r.err
}

func TestAnswerPromptThroughApproval(t *testing.T) {
	hookCard := ApprovalWire{ID: "a1", Options: []ApprovalOptionWire{{ID: "allow"}, {ID: "deny"}}}
	cases := []struct {
		name        string
		approvals   *recordingApprovals
		number      int
		wantHandled bool
		wantDecided []string
		wantErr     bool
	}{
		{name: "yes allows the hook's request", approvals: &recordingApprovals{pending: []ApprovalWire{hookCard}}, number: 1, wantHandled: true, wantDecided: []string{"a1:allow"}},
		{name: "no denies it", approvals: &recordingApprovals{pending: []ApprovalWire{hookCard}}, number: 3, wantHandled: true, wantDecided: []string{"a1:deny"}},
		{name: "a remember-it option has no hook form", approvals: &recordingApprovals{pending: []ApprovalWire{hookCard}}, number: 2},
		{name: "nothing pending means keys", approvals: &recordingApprovals{}, number: 1},
		{name: "two pending are ambiguous", approvals: &recordingApprovals{pending: []ApprovalWire{hookCard, hookCard}}, number: 1},
		{name: "a refusal is surfaced", approvals: &recordingApprovals{pending: []ApprovalWire{hookCard}, err: &ApprovalDecisionError{Code: "approval_with_brain"}}, number: 1, wantDecided: []string{"a1:allow"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			s.SetAgentApprovals(tc.approvals)
			handled, err := s.answerPromptThroughApproval("%1", *permissionPrompt(), tc.number)
			if handled != tc.wantHandled || (err != nil) != tc.wantErr || !reflect.DeepEqual(tc.approvals.decided, tc.wantDecided) {
				t.Fatalf("handled=%v err=%v decided=%v", handled, err, tc.approvals.decided)
			}
		})
	}
}
