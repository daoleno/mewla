package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// runWorkerAnswer answers the prompt open in a Worker's terminal: a
// permission, plan, startup or approval menu by option number, or the
// questions of an AskUserQuestion by their answers.
func runWorkerAnswer(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla worker answer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "worker_answer"}
	var answers string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.StringVar(&req.WorkerID, "id", "", "Worker session id")
	fs.StringVar(&req.PromptID, "prompt", "", "prompt id from the needs_input event or `mewla worker status`")
	fs.IntVar(&req.PromptChoice, "choice", 0, "number of the option to pick in a permission, plan, startup or approval menu")
	fs.StringVar(&answers, "answers", "", `answers to a question prompt: a JSON array with one {"selected":[<1-based option numbers>],"other":"text"} per question`)
	fs.BoolVar(&req.Force, "force", false, "answer a non-delegated external session")
	fs.StringVar(&req.ApprovalBasis, "basis", "", "what the answer relies on: the brief sentence that authorizes allowing a blocked action (required to allow one Brain holds), or why it is escalated or denied")
	fs.BoolVar(&req.PromptEscalate, "escalate", false, "hand a blocked action Brain holds to the user instead of answering it (with -basis saying why)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla worker answer -id <worker> -prompt <id> (-choice <n> | -answers '<json>' | -escalate) [-basis \"<brief sentence>\"]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.PromptID) == "" {
		return fmt.Errorf("-id and -prompt are required")
	}
	raw, err := promptAnswersFlag(req.PromptChoice, answers, req.PromptEscalate)
	if err != nil {
		return err
	}
	req.PromptAnswers = raw
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, true)
}

// promptAnswersFlag checks that exactly one of -choice and -answers was given
// and turns -answers, whose option numbers start at 1 as the prompt shows
// them, into the zero-based indexes the daemon takes.
func promptAnswersFlag(choice int, answers string, escalate bool) (json.RawMessage, error) {
	answers = strings.TrimSpace(answers)
	given := 0
	for _, present := range []bool{choice > 0, answers != "", escalate} {
		if present {
			given++
		}
	}
	if given != 1 {
		return nil, fmt.Errorf("give exactly one of -choice <n>, -answers '<json>' and -escalate")
	}
	if answers == "" {
		return nil, nil
	}
	var parsed []struct {
		Selected []int  `json:"selected"`
		Other    string `json:"other"`
	}
	if err := json.Unmarshal([]byte(answers), &parsed); err != nil || len(parsed) == 0 {
		return nil, fmt.Errorf(`-answers must be a JSON array with one {"selected":[<option numbers>],"other":"text"} per question`)
	}
	converted := make([]server.ChoiceSelection, len(parsed))
	for index, answer := range parsed {
		converted[index] = server.ChoiceSelection{Selected: []int{}, Other: answer.Other}
		for _, number := range answer.Selected {
			if number < 1 {
				return nil, fmt.Errorf("question %d: option numbers start at 1", index+1)
			}
			converted[index].Selected = append(converted[index].Selected, number-1)
		}
	}
	return json.Marshal(converted)
}

func (a *controlApp) handleWorkerAnswer(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	if workerID == "" {
		return control.ErrorResponse("missing_worker_id", "Worker id is required.")
	}
	worker, ownershipErr := a.resolveOwnedWorker(workerID)
	if ownershipErr != nil {
		return control.ErrorResponse(workerOwnershipErrorCode(ownershipErr), ownershipErr.Error())
	}
	if worker == nil {
		return control.ErrorResponse("worker_not_found", "Worker session was not found.")
	}
	if !worker.Delegated && !worker.Hidden && !req.Force {
		return control.ErrorResponse("worker_not_delegated", "Refusing to answer a session that was not created as a Brain delegated Mewla Worker. Use -force only when you intentionally want to control this external session.")
	}
	if approvalID, held := a.heldApprovalFor(workerID, req.PromptID); held {
		return a.answerHeldApproval(workerID, approvalID, req)
	}
	pane, ok := a.watcher.(server.PanePrompts)
	if !ok {
		return control.ErrorResponse("prompt_unavailable", "Prompts cannot be answered here.")
	}
	if req.PromptEscalate {
		// The dialog may be on screen as well as held; its prompt is then the
		// one the pane shows, and the hook's decision is what is handed over.
		open, err := pane.CurrentPrompt(workerID)
		if err != nil {
			return promptAnswerError(err)
		}
		var held []control.Approval
		if a.agentEvents != nil {
			held = a.agentEvents.approvals(workerID, false)
		}
		if open != nil && open.ID == strings.TrimSpace(req.PromptID) && open.Kind == classifier.PromptPermission && len(held) == 1 {
			return a.answerHeldApproval(workerID, held[0].ID, req)
		}
		return control.ErrorResponse("prompt_gone", "No blocked action is held for that prompt; only a blocked action Brain holds can be escalated.")
	}
	answer := server.PromptAnswer{PromptID: strings.TrimSpace(req.PromptID), Number: req.PromptChoice}
	if len(req.PromptAnswers) > 0 {
		if err := json.Unmarshal(req.PromptAnswers, &answer.Answers); err != nil {
			return control.ErrorResponse("invalid_request", "Prompt answers must be a JSON array.")
		}
	}
	if len(answer.Answers) == 0 && answer.Number < 1 {
		return control.ErrorResponse("invalid_request", "Give an option number or the answers to the questions.")
	}
	hook := func(prompt classifier.Prompt, number int) (bool, error) {
		return a.answerPromptThroughApproval(workerID, prompt, number, req.ApprovalBasis)
	}
	recorded, err := server.AnswerPanePrompt(pane, workerID, answer, hook, a.choiceConversationLoader(worker))
	if err != nil {
		return promptAnswerError(err)
	}
	resp := control.Response{OK: true, Confirmation: "answered"}
	if len(recorded) > 0 {
		resp.Context = map[string]any{"answers": recorded}
	}
	return resp
}

// heldApprovalFor finds the decision a permission hook holds for the Worker
// that the prompt id names. A sub-agent's dialog is not drawn in the pane, so
// this is the only place such a prompt exists.
func (a *controlApp) heldApprovalFor(workerID, promptID string) (string, bool) {
	if a.agentEvents == nil {
		return "", false
	}
	for _, pending := range a.agentEvents.approvals(workerID, false) {
		if approvalPromptID(pending.ID) == promptID {
			return pending.ID, true
		}
	}
	return "", false
}

// answerHeldApproval answers a held decision by its prompt: the numbered
// option, or escalate. The decision is the same one the app and the hook
// path use, so the Work records who answered and on what basis.
func (a *controlApp) answerHeldApproval(workerID, approvalID string, req control.Request) control.Response {
	optionID := "escalate"
	if !req.PromptEscalate {
		if len(req.PromptAnswers) > 0 || req.PromptChoice < 1 {
			return control.ErrorResponse("prompt_refused", "A permission prompt is answered with -choice <option number> or -escalate.")
		}
		optionID = ""
		for _, pending := range a.agentEvents.approvals(workerID, false) {
			if pending.ID == approvalID && req.PromptChoice <= len(pending.Options) {
				optionID = pending.Options[req.PromptChoice-1].ID
			}
		}
		if optionID == "" {
			return control.ErrorResponse("prompt_refused", fmt.Sprintf("the prompt has no option %d", req.PromptChoice))
		}
	}
	resp := a.agentEvents.decide(workerID, approvalID, optionID, answeredByOperator, req.ApprovalBasis)
	if resp.OK {
		resp.Confirmation = "answered"
	}
	return resp
}

func promptAnswerError(err error) control.Response {
	var refused *server.ApprovalDecisionError
	switch {
	case errors.As(err, &refused):
		return control.ErrorResponse(refused.Code, refused.Message)
	case server.PromptGone(err):
		return control.ErrorResponse("prompt_gone", watcher.ErrPromptGone.Error())
	case errors.Is(err, watcher.ErrClaudeChoiceRefused):
		return control.ErrorResponse("prompt_refused", strings.TrimPrefix(err.Error(), watcher.ErrClaudeChoiceRefused.Error()+": "))
	}
	return control.ErrorResponse("prompt_failed", err.Error())
}

// answerPromptThroughApproval resolves a permission prompt through the
// decision its hook is waiting on, so the hub, the Work's audit trail and
// Brain's review see the answer. A prompt without a hook decision is left to
// the pane.
func (a *controlApp) answerPromptThroughApproval(workerID string, prompt classifier.Prompt, number int, basis string) (bool, error) {
	if a.agentEvents == nil {
		return false, nil
	}
	optionID, ok := server.ApprovalOptionForPrompt(prompt, number)
	if !ok {
		return false, nil
	}
	pending := a.agentEvents.approvals(workerID, false)
	if len(pending) != 1 {
		return false, nil
	}
	offered := false
	for _, option := range pending[0].Options {
		offered = offered || option.ID == optionID
	}
	if !offered {
		return false, nil
	}
	resp := a.agentEvents.decide(workerID, pending[0].ID, optionID, answeredByOperator, basis)
	if resp.OK {
		return true, nil
	}
	if resp.Error == nil {
		return false, fmt.Errorf("the decision was not taken")
	}
	return false, &server.ApprovalDecisionError{Code: resp.Error.Code, Message: resp.Error.Message}
}

// choiceConversationLoader reads the transcript a question prompt is answered
// against: the provider's own record, never the caller's account of it.
func (a *controlApp) choiceConversationLoader(worker *classifier.Worker) func() (work.CodexConversation, error) {
	reader := work.NewProviderConversationReader()
	provider := providerOf(a.execs, worker.ExecutorID, worker.Command)
	snapshot := *worker
	return func() (work.CodexConversation, error) {
		if provider != work.WorkerProviderClaude {
			return work.CodexConversation{}, fmt.Errorf("%w: this Session is not a live Claude Session", watcher.ErrClaudeChoiceRefused)
		}
		conversation, err := reader.Load(snapshot, provider, time.Now())
		if err != nil || a.brainService == nil || a.brainService.CurrentHostSessionID() != snapshot.ID {
			return conversation, err
		}
		if bound, boundErr := a.brainService.HostBoundProviderConversation(); boundErr == nil && bound.Available {
			conversation = work.PreferHostBoundConversation(conversation, bound)
		}
		return conversation, nil
	}
}
