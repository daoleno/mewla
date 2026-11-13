package server

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/attachment"
	"github.com/daoleno/mewla/daemon/watcher"
)

// A create_session request may carry the Session's first message (`text`,
// and `attachments` as send_input takes them). The daemon answers
// session_created at once with `first_message: "pending"` and holds the
// message until the new agent's input prompt is ready, then submits it as the
// person's first turn. How that went is broadcast to every connection as
// session_first_message, since the asking phone may be gone by then:
//
//   - "sent": the agent took it.
//   - "not_sent": it never reached the agent (the agent was not ready within
//     the budget, or the Session ended first); the text comes back for the
//     composer.
//   - "unknown": it may have reached the agent; the text comes back, but the
//     app must not send it again on its own.
//
// The daemon keeps no copy: a restart while it waits loses the message, so the
// app keeps the text as that Session's draft until it hears "sent".

// firstMessageBudget is how long a first message waits for its agent. A new
// folder opens Claude on "Trust this folder?", which waits for the person
// (from the phone, in the tray), so the wait is the person's time, not an
// agent's start-up time. The wait ends at once if the Session goes away.
const firstMessageBudget = 10 * time.Minute

const (
	firstMessageSent    = "sent"
	firstMessageNotSent = "not_sent"
	firstMessageUnknown = "unknown"
)

// firstMessageRequest is a create_session's first message, validated before
// any Session exists.
type firstMessageRequest struct {
	text        string
	attachments []attachment.File
	rendered    string
}

// firstMessageError is a first message refused before a Session was created.
type firstMessageError struct {
	code    string
	message string
}

func (e *firstMessageError) Error() string { return e.message }

// prepareFirstMessage validates a create_session's first message against the
// command the Session will start with. No text and no attachments is no first
// message (nil, nil).
func (s *Server) prepareFirstMessage(command, text string, attachments []attachment.File) (*firstMessageRequest, error) {
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return nil, nil
	}
	if !watcher.HoldsFirstMessage(command) {
		return nil, &firstMessageError{code: "first_message_unsupported", message: "A first message needs an agent; this command does not start one."}
	}
	files, err := s.uploadStore.ConfineAll(attachments)
	if err != nil {
		return nil, &firstMessageError{code: "attachment_not_uploaded", message: err.Error()}
	}
	rendered := attachment.Message{Text: text, Files: files}.Render()
	if strings.TrimSpace(rendered) == "" {
		return nil, &firstMessageError{code: "first_message_empty", message: "Message is empty."}
	}
	return &firstMessageRequest{text: text, attachments: attachments, rendered: rendered}, nil
}

// firstMessageOutcome reads a ready-then-submit result. Only a proven
// non-submission is "not_sent"; anything that may have reached the agent is
// "unknown" and never replayed.
func firstMessageOutcome(err error) string {
	if err == nil {
		return firstMessageSent
	}
	var submission *watcher.InputSubmissionError
	if errors.As(err, &submission) && submission.Result.Outcome == watcher.InputNotSubmitted {
		return firstMessageNotSent
	}
	return firstMessageUnknown
}

// deliverFirstMessage waits for the new Session's agent and submits its first
// message, then tells every connection how it went. It runs off the request's
// goroutine and holds no server lock.
func (s *Server) deliverFirstMessage(workerID, command, requestID string, message *firstMessageRequest) {
	send := s.firstMessageSend
	if send == nil {
		send = func(workerID, command, text string) error {
			return s.watcher.SendInputWhenReadyBudgeted(workerID, command, text, firstMessageBudget)
		}
	}
	// The final newline submits it, as send_input's does.
	err := send(workerID, command, strings.TrimRight(message.rendered, "\r\n")+"\n")
	outcome := firstMessageOutcome(err)
	event := map[string]any{
		"type":       "session_first_message",
		"worker_id":  workerID,
		"request_id": requestID,
		"outcome":    outcome,
	}
	if outcome != firstMessageSent {
		event["text"] = message.text
		if len(message.attachments) > 0 {
			event["attachments"] = message.attachments
		}
		event["message"] = err.Error()
		log.Printf("create_session first message %s worker=%s request=%s: %v", outcome, workerID, requestID, err)
	}
	s.broadcastJSON(event)
}
