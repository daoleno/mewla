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
	fs.StringVar(&req.ApprovalBasis, "basis", "", "the brief sentence that authorizes allowing a blocked action (required when Brain holds a permission request)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla worker answer -id <worker> -prompt <id> (-choice <n> | -answers '<json>') [-basis \"<brief sentence>\"]")
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
	raw, err := promptAnswersFlag(req.PromptChoice, answers)
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
func promptAnswersFlag(choice int, answers string) (json.RawMessage, error) {
	answers = strings.TrimSpace(answers)
	if (choice > 0) == (answers != "") {
		return nil, fmt.Errorf("give exactly one of -choice <n> and -answers '<json>'")
	}
	if answers == "" {
		return nil, nil
	}
	var given []struct {
		Selected []int  `json:"selected"`
		Other    string `json:"other"`
	}
	if err := json.Unmarshal([]byte(answers), &given); err != nil || len(given) == 0 {
		return nil, fmt.Errorf(`-answers must be a JSON array with one {"selected":[<option numbers>],"other":"text"} per question`)
	}
	converted := make([]server.ChoiceSelection, len(given))
	for index, answer := range given {
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
	pane, ok := a.watcher.(server.PanePrompts)
	if !ok {
		return control.ErrorResponse("prompt_unavailable", "Prompts cannot be answered here.")
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
