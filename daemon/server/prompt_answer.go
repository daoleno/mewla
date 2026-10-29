package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// PromptAnswer answers the prompt a Worker's pane shows. PromptID names the
// prompt the answer was composed for; the answer is refused, and no key sent,
// when the pane no longer shows it. A menu prompt (permission, plan, startup,
// approval) takes Number, the digit of the option. A choice prompt takes
// Answers, one per question, with zero-based option indexes.
type PromptAnswer struct {
	PromptID string            `json:"prompt_id"`
	Number   int               `json:"number,omitempty"`
	Answers  []ChoiceSelection `json:"answers,omitempty"`
}

// PanePrompts is the part of the watcher that answers pane prompts.
type PanePrompts interface {
	CurrentPrompt(sessionID string) (*classifier.Prompt, error)
	AnswerPrompt(sessionID, promptID string, number int) error
	AnswerClaudeChoice(sessionID string, questions []watcher.ClaudeChoiceQuestion, answers []watcher.ClaudeChoiceAnswer) error
}

// PromptHook lets an owner that holds the prompt's question out of band, such
// as a permission hook waiting on a decision, answer it without a keystroke.
// handled=false leaves the answer to the pane.
type PromptHook func(prompt classifier.Prompt, number int) (handled bool, err error)

// PromptGone reports that the prompt an answer named is no longer the one open.
func PromptGone(err error) bool {
	return errors.Is(err, watcher.ErrPromptGone) || errors.Is(err, watcher.ErrClaudeChoiceGone)
}

// AnswerPanePrompt is the one path an answer to a pane prompt takes, for the
// app and for `mewla worker answer`. It re-reads the pane, refuses a prompt
// that is not the named one, and then answers through the hook, the choice
// driver or the menu key. A choice returns the answers Claude recorded.
func AnswerPanePrompt(pane PanePrompts, workerID string, answer PromptAnswer, hook PromptHook, load func() (work.CodexConversation, error)) ([]string, error) {
	answer.PromptID = strings.TrimSpace(answer.PromptID)
	if workerID = strings.TrimSpace(workerID); workerID == "" || answer.PromptID == "" {
		return nil, fmt.Errorf("%w: name the Worker and the prompt", watcher.ErrClaudeChoiceRefused)
	}
	prompt, err := pane.CurrentPrompt(workerID)
	if err != nil {
		return nil, err
	}
	if prompt == nil || prompt.ID != answer.PromptID {
		return nil, fmt.Errorf("%w: %w", watcher.ErrPromptGone, watcher.ErrClaudeChoiceRefused)
	}
	if prompt.Kind == classifier.PromptChoice {
		if len(answer.Answers) == 0 {
			return nil, fmt.Errorf("%w: a question is answered with its answers", watcher.ErrClaudeChoiceRefused)
		}
		conversation, err := load()
		if err != nil {
			return nil, err
		}
		pending, ok := work.LatestPendingConversationChoice(conversation)
		if !ok {
			return nil, watcher.ErrClaudeChoiceGone
		}
		return driveChoiceAnswer(workerID, answerChoiceRequest{CallID: pending.CallID, Answers: answer.Answers}, load, pane.AnswerClaudeChoice)
	}
	if hook != nil {
		handled, err := hook(*prompt, answer.Number)
		if err != nil || handled {
			return nil, err
		}
	}
	return nil, pane.AnswerPrompt(workerID, answer.PromptID, answer.Number)
}

// ApprovalOptionForPrompt maps a permission menu option to the decision a
// permission hook is waiting on: "Yes" allows once, "No..." denies. The
// remember-for-the-session variants have no hook form.
func ApprovalOptionForPrompt(prompt classifier.Prompt, number int) (string, bool) {
	if prompt.Kind != classifier.PromptPermission {
		return "", false
	}
	for _, option := range prompt.Options {
		if option.Number != number {
			continue
		}
		label := strings.ToLower(strings.TrimRight(strings.TrimSpace(option.Label), ".!"))
		switch {
		case label == "yes":
			return "allow", true
		case label == "no" || strings.HasPrefix(label, "no,"):
			return "deny", true
		}
	}
	return "", false
}

// PromptAnswerCode names the failure of an answer: the prompt has passed,
// the answer was refused, or driving it failed.
func PromptAnswerCode(err error) (code, message string) {
	switch {
	case PromptGone(err):
		return "answer_prompt_gone", "The prompt has passed."
	case errors.Is(err, watcher.ErrClaudeChoiceRefused):
		message = strings.TrimPrefix(err.Error(), watcher.ErrClaudeChoiceRefused.Error()+": ")
		if message == "" {
			return "answer_prompt_refused", "The answer was refused."
		}
		return "answer_prompt_refused", strings.ToUpper(message[:1]) + message[1:] + "."
	}
	var refused *ApprovalDecisionError
	if errors.As(err, &refused) {
		return "answer_prompt_refused", refused.Message
	}
	return "answer_prompt_failed", err.Error()
}

func (s *Server) handleAnswerPrompt(conn *websocket.Conn, raw clientMessage) {
	var answer PromptAnswer
	if len(raw.PromptAnswer) == 0 || json.Unmarshal(raw.PromptAnswer, &answer) != nil || strings.TrimSpace(answer.PromptID) == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "answer_prompt_invalid", "missing prompt answer")
		return
	}
	resolved := s.resolveCodexConversationWorker(raw)
	if !resolved.ready || !resolved.fromWatcher {
		s.sendErrorWithRequestID(conn, raw.RequestID, "answer_prompt_refused", "This Session is not a live Session.")
		return
	}
	var pane PanePrompts = s.watcher
	if s.promptPaneOverride != nil {
		pane = s.promptPaneOverride
	}
	hook := func(prompt classifier.Prompt, number int) (bool, error) {
		return s.answerPromptThroughApproval(resolved.targetID, prompt, number)
	}
	if _, err := AnswerPanePrompt(pane, resolved.targetID, answer, hook, s.choiceConversationLoader(resolved)); err != nil {
		code, message := PromptAnswerCode(err)
		s.sendErrorWithRequestID(conn, raw.RequestID, code, message)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "prompt_answered",
		"request_id": raw.RequestID,
		"worker_id":  resolved.targetID,
		"prompt_id":  answer.PromptID,
	})
}

// answerPromptThroughApproval resolves a permission prompt through the
// decision its hook is waiting on, when the app holds exactly that one.
func (s *Server) answerPromptThroughApproval(workerID string, prompt classifier.Prompt, number int) (bool, error) {
	optionID, ok := ApprovalOptionForPrompt(prompt, number)
	if !ok {
		return false, nil
	}
	pending := s.pendingApprovals(workerID)
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
	s.mu.Lock()
	approvals := s.agentApprovals
	s.mu.Unlock()
	if approvals == nil {
		return false, nil
	}
	if err := approvals.Decide(workerID, pending[0].ID, optionID); err != nil {
		return false, err
	}
	return true, nil
}

// choiceConversationLoader reads the transcript a choice is answered against.
func (s *Server) choiceConversationLoader(resolved resolvedCodexConversationWorker) func() (work.CodexConversation, error) {
	reader := work.NewProviderConversationReader()
	return func() (work.CodexConversation, error) {
		conversation, err := s.loadProviderConversation(reader, resolved, time.Now())
		if err != nil || s.brain == nil || s.brain.CurrentHostSessionID() != resolved.targetID {
			return conversation, err
		}
		// Brain chat shows the host-bound transcript; answer against that same source.
		if bound, boundErr := s.brain.HostBoundProviderConversation(); boundErr == nil && bound.Available {
			conversation = work.PreferHostBoundConversation(conversation, bound)
		}
		return conversation, nil
	}
}
