package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// answerChoiceRequest is the client payload for answer_choice. It names the
// pending tool call and picks option indexes; the questions themselves are
// re-read from the provider transcript, never trusted from the client.
type answerChoiceRequest struct {
	CallID  string `json:"call_id"`
	Answers []struct {
		Selected []int  `json:"selected"`
		Other    string `json:"other"`
	} `json:"answers"`
}

const answerChoiceConfirmTimeout = 8 * time.Second

func (s *Server) handleAnswerChoice(conn *websocket.Conn, raw clientMessage) {
	var request answerChoiceRequest
	if len(raw.ChoiceAnswer) == 0 || json.Unmarshal(raw.ChoiceAnswer, &request) != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "answer_choice_invalid", "missing choice answer")
		return
	}
	resolved := s.resolveCodexConversationWorker(raw)
	if !resolved.ready || !resolved.fromWatcher || resolved.provider != work.WorkerProviderClaude {
		s.sendErrorWithRequestID(conn, raw.RequestID, "answer_choice_refused", "This Session is not a live Claude Session.")
		return
	}
	reader := work.NewProviderConversationReader()
	load := func() (work.CodexConversation, error) {
		return s.loadProviderConversation(reader, resolved, time.Now())
	}
	answers, err := s.answerChoice(resolved.targetID, request, load)
	if err != nil {
		code, message := "answer_choice_failed", err.Error()
		if errors.Is(err, watcher.ErrClaudeChoiceRefused) {
			code = "answer_choice_refused"
			message = strings.TrimPrefix(message, watcher.ErrClaudeChoiceRefused.Error()+": ")
			message = strings.ToUpper(message[:1]) + message[1:] + "."
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, code, message)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "choice_answered",
		"request_id": raw.RequestID,
		"worker_id":  resolved.targetID,
		"call_id":    request.CallID,
		"answers":    answers,
	})
}

// answerChoice drives the pending choice and returns the answers Claude
// recorded, after proving they are exactly the requested ones.
func (s *Server) answerChoice(workerID string, request answerChoiceRequest, load func() (work.CodexConversation, error)) ([]string, error) {
	conversation, err := load()
	if err != nil {
		return nil, err
	}
	event, ok := work.PendingConversationChoice(conversation, request.CallID)
	if !ok {
		return nil, fmt.Errorf("%w: this question is no longer waiting for an answer", watcher.ErrClaudeChoiceRefused)
	}
	questions := make([]watcher.ClaudeChoiceQuestion, len(event.Choice.Questions))
	for index, question := range event.Choice.Questions {
		options := make([]string, len(question.Options))
		for option := range question.Options {
			options[option] = question.Options[option].Label
		}
		questions[index] = watcher.ClaudeChoiceQuestion{
			Question:    question.Question,
			MultiSelect: question.MultiSelect,
			Options:     options,
			AllowsOther: question.AllowsOther(),
		}
	}
	answers := make([]watcher.ClaudeChoiceAnswer, len(request.Answers))
	for index, answer := range request.Answers {
		answers[index] = watcher.ClaudeChoiceAnswer{Selected: answer.Selected, Other: answer.Other}
	}
	expected, err := watcher.ExpectedClaudeChoiceAnswers(questions, answers)
	if err != nil {
		return nil, err
	}
	drive := s.answerClaudeChoiceOverride
	if drive == nil {
		drive = s.watcher.AnswerClaudeChoice
	}
	if err := drive(workerID, questions, answers); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(answerChoiceConfirmTimeout)
	for {
		conversation, err := load()
		if err == nil {
			if recorded, settled := settledConversationChoice(conversation, request.CallID); settled {
				if recorded.State != work.ConversationChoiceAnswered {
					return nil, fmt.Errorf("Claude recorded the question as %s", recorded.State)
				}
				for index, parts := range expected {
					if index >= len(recorded.Answers) || !watcher.ClaudeChoiceAnswerMatches(recorded.Answers[index], parts) {
						return recorded.Answers, fmt.Errorf("Claude recorded %q for question %d, expected %q", strings.Join(recorded.Answers, " / "), index+1, strings.Join(parts, ", "))
					}
				}
				return recorded.Answers, nil
			}
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("the answer was submitted but Claude has not recorded it yet")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func settledConversationChoice(conversation work.CodexConversation, callID string) (work.ConversationChoice, bool) {
	for _, event := range conversation.Events {
		if event.CallID == callID && event.Choice != nil && event.Choice.State != work.ConversationChoicePending {
			return *event.Choice, true
		}
	}
	return work.ConversationChoice{}, false
}
