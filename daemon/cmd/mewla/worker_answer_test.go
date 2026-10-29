package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// promptWatcher is a control watcher whose pane shows a fixed prompt.
type promptWatcher struct {
	*fakeControlWatcher
	prompt  *classifier.Prompt
	pressed []int
}

func (p *promptWatcher) CurrentPrompt(string) (*classifier.Prompt, error) { return p.prompt, nil }

func (p *promptWatcher) AnswerPrompt(_, promptID string, number int) error {
	if p.prompt == nil || p.prompt.ID != promptID {
		return fmt.Errorf("%w: %w", watcher.ErrPromptGone, watcher.ErrClaudeChoiceRefused)
	}
	p.pressed = append(p.pressed, number)
	return nil
}

func (p *promptWatcher) AnswerClaudeChoice(string, []watcher.ClaudeChoiceQuestion, []watcher.ClaudeChoiceAnswer) error {
	return errors.New("not driven in this test")
}

func newPromptApp(prompt *classifier.Prompt) (*controlApp, *promptWatcher) {
	fw := &promptWatcher{fakeControlWatcher: newFakeControlWatcher(), prompt: prompt}
	fw.workers["%4"] = &classifier.Worker{ID: "%4", Name: "Franklin", Delegated: true, Command: "claude", State: classifier.StateBlocked}
	return &controlApp{watcher: fw}, fw
}

func planPrompt() *classifier.Prompt {
	return &classifier.Prompt{ID: "plan1", Kind: classifier.PromptPlan, Options: []classifier.PromptOption{{Number: 1, Label: "Yes, proceed"}, {Number: 2, Label: "No, keep planning"}}}
}

func TestWorkerAnswerPressesTheOptionOfTheNamedPromptOnly(t *testing.T) {
	app, fw := newPromptApp(planPrompt())
	resp := app.HandleControlRequest(control.Request{Type: "worker_answer", WorkerID: "%4", PromptID: "plan1", PromptChoice: 2})
	if !resp.OK || resp.Confirmation != "answered" || !reflect.DeepEqual(fw.pressed, []int{2}) {
		t.Fatalf("resp=%+v pressed=%v", resp, fw.pressed)
	}

	stale := app.HandleControlRequest(control.Request{Type: "worker_answer", WorkerID: "%4", PromptID: "old", PromptChoice: 1})
	if stale.OK || stale.Error.Code != "prompt_gone" || stale.Error.Message != watcher.ErrPromptGone.Error() || len(fw.pressed) != 1 {
		t.Fatalf("stale prompt: resp=%+v pressed=%v", stale, fw.pressed)
	}
	if err := controlResponseError(stale); err == nil || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "the prompt has passed") {
		t.Fatalf("CLI error = %v", err)
	}

	fw.prompt = nil
	if gone := app.HandleControlRequest(control.Request{Type: "worker_answer", WorkerID: "%4", PromptID: "plan1", PromptChoice: 1}); gone.OK || gone.Error.Code != "prompt_gone" || len(fw.pressed) != 1 {
		t.Fatalf("no prompt open: %+v pressed=%v", gone, fw.pressed)
	}
}

func TestWorkerAnswerRefusesIncompleteAndForeignRequests(t *testing.T) {
	app, fw := newPromptApp(planPrompt())
	fw.workers["%9"] = &classifier.Worker{ID: "%9", Command: "claude"}
	cases := []struct {
		name string
		req  control.Request
		code string
	}{
		{"no option", control.Request{WorkerID: "%4", PromptID: "plan1"}, "invalid_request"},
		{"unknown worker", control.Request{WorkerID: "%7", PromptID: "plan1", PromptChoice: 1}, "worker_not_found"},
		{"external session", control.Request{WorkerID: "%9", PromptID: "plan1", PromptChoice: 1}, "worker_not_delegated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Type = "worker_answer"
			resp := app.HandleControlRequest(tc.req)
			if resp.OK || resp.Error.Code != tc.code {
				t.Fatalf("resp = %+v", resp)
			}
		})
	}
	if len(fw.pressed) != 0 {
		t.Fatalf("a refused request pressed %v", fw.pressed)
	}
}

func TestWorkerSendRefusesWhileAPromptIsOpen(t *testing.T) {
	prompt := planPrompt()
	app, fw := newPromptApp(prompt)
	resp := app.HandleControlRequest(control.Request{Type: "worker_send", WorkerID: "%4", Text: "1", Submit: true})
	if resp.OK || resp.Error.Code != "prompt_open" {
		t.Fatalf("resp = %+v", resp)
	}
	for _, want := range []string{"%4", "plan1", "mewla worker answer -id %4 -prompt plan1"} {
		if !strings.Contains(resp.Error.Message, want) {
			t.Fatalf("message %q lacks %q", resp.Error.Message, want)
		}
	}
	if len(fw.sent) != 0 || len(fw.submitted) != 0 {
		t.Fatalf("text reached the pane: %+v %+v", fw.sent, fw.submitted)
	}

	// The cached projection alone also blocks, and a Work follow-up is no exception.
	fw.prompt = nil
	fw.workers["%4"].Prompt = prompt
	if resp := app.HandleControlRequest(control.Request{Type: "worker_send", WorkerID: "%4", WorkID: "work-1", Text: "go", Submit: true}); resp.OK || resp.Error.Code != "prompt_open" {
		t.Fatalf("work follow-up: %+v", resp)
	}

	// With the prompt gone, sending works as before.
	fw.workers["%4"].Prompt = nil
	if resp := app.HandleControlRequest(control.Request{Type: "worker_send", WorkerID: "%4", Text: "continue", Submit: true}); !resp.OK {
		t.Fatalf("send after the prompt: %+v", resp)
	}
}

func TestWorkerAnswerResolvesAHookHeldPermissionThroughTheHub(t *testing.T) {
	s, _ := gateService(t, false)
	done := start(s, askedPush)
	asked := pending(t, s, false, 1)[0]

	prompt := &classifier.Prompt{ID: "perm1", Kind: classifier.PromptPermission, Options: []classifier.PromptOption{
		{Number: 1, Label: "Yes"}, {Number: 2, Label: "Yes, and don't ask again"}, {Number: 3, Label: "No"},
	}}
	app, fw := newPromptApp(prompt)
	app.agentEvents = s

	resp := app.HandleControlRequest(control.Request{Type: "worker_answer", WorkerID: "%4", PromptID: "perm1", PromptChoice: 3})
	if !resp.OK || len(fw.pressed) != 0 {
		t.Fatalf("hook answer: resp=%+v pressed=%v", resp, fw.pressed)
	}
	if out := <-done; !strings.Contains(out.HookOutput, `"deny"`) {
		t.Fatalf("the hook was not told: %+v (approval %s)", out, asked.ID)
	}

	// With nothing held by a hook the same answer is a keystroke.
	resp = app.HandleControlRequest(control.Request{Type: "worker_answer", WorkerID: "%4", PromptID: "perm1", PromptChoice: 3})
	if !resp.OK || !reflect.DeepEqual(fw.pressed, []int{3}) {
		t.Fatalf("keystroke fallback: resp=%+v pressed=%v", resp, fw.pressed)
	}
}

func TestPromptAnswersFlag(t *testing.T) {
	cases := []struct {
		name     string
		choice   int
		answers  string
		escalate bool
		want     string
		wantErr  bool
	}{
		{name: "menu choice carries no answers", choice: 2},
		{name: "one-based numbers become indexes", answers: `[{"selected":[1,3]},{"selected":[],"other":"Teal"}]`, want: `[{"selected":[0,2],"other":""},{"selected":[],"other":"Teal"}]`},
		{name: "neither", wantErr: true},
		{name: "escalate alone", escalate: true},
		{name: "escalate with a choice", choice: 1, escalate: true, wantErr: true},
		{name: "both", choice: 1, answers: `[{"selected":[1]}]`, wantErr: true},
		{name: "zero is not an option number", answers: `[{"selected":[0]}]`, wantErr: true},
		{name: "not an array", answers: `{"selected":[1]}`, wantErr: true},
		{name: "empty array", answers: `[]`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := promptAnswersFlag(tc.choice, tc.answers, tc.escalate)
			if (err != nil) != tc.wantErr || string(raw) != tc.want {
				t.Fatalf("raw=%s err=%v", raw, err)
			}
		})
	}
}

func TestPromptWithTranscriptChoiceListsEveryQuestion(t *testing.T) {
	pane := classifier.Prompt{ID: "c1", Kind: classifier.PromptChoice, Questions: []classifier.PromptQuestion{{Header: "Fruit"}, {Header: "Color"}}}
	conversation := work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{{
		CallID: "toolu_9",
		Choice: &work.ConversationChoice{State: work.ConversationChoicePending, Questions: []work.ConversationChoiceQuestion{
			{Header: "Fruit", Question: "Which fruit?", Options: []work.ConversationChoiceOption{{Label: "Apple", Description: "crisp"}, {Label: "Banana"}}},
			{Header: "Color", Question: "Which colors?", MultiSelect: true, Options: []work.ConversationChoiceOption{{Label: "Red", Preview: "r"}}},
		}},
	}}}
	got := promptWithTranscriptChoice(pane, conversation)
	if got.ID != "c1" || got.CallID != "toolu_9" || len(got.Questions) != 2 {
		t.Fatalf("prompt = %+v", got)
	}
	first, second := got.Questions[0], got.Questions[1]
	if first.Header != "Fruit" || first.Question != "Which fruit?" || !first.AllowsOther || first.Options[0].Description != "crisp" || first.Options[1].Number != 2 {
		t.Fatalf("first = %+v", first)
	}
	if !second.MultiSelect || second.AllowsOther {
		t.Fatalf("second = %+v", second)
	}
	encoded, _ := json.Marshal(got)
	if !strings.Contains(string(encoded), "Which colors?") {
		t.Fatalf("Brain would not see every question: %s", encoded)
	}

	conversation.Events[0].Choice.State = work.ConversationChoiceAnswered
	if untouched := promptWithTranscriptChoice(pane, conversation); !reflect.DeepEqual(untouched, pane) {
		t.Fatalf("an answered choice must not enrich: %+v", untouched)
	}
}
