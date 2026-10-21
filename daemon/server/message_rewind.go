package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// rewindMessageRequest is the client payload for rewind_message. It names the
// transcript event of the user's prompt; the daemon works out how far back the
// CLI has to step from its own copy of the conversation.
type rewindMessageRequest struct {
	EventID string `json:"event_id"`
}

// messageRewindProviders can step a conversation back to an earlier prompt with
// their own CLI. Every other agent only offers an edited copy as a new message.
func messageRewindProvider(provider string) (string, bool) {
	switch provider {
	case work.WorkerProviderClaude:
		return watcher.MessageRewindClaude, true
	case work.WorkerProviderCodex:
		return watcher.MessageRewindCodex, true
	case work.WorkerProviderPi:
		return watcher.MessageRewindPi, true
	}
	return "", false
}

// rewoundMessages remembers, per Session, the prompt a rewind stepped back to.
// Claude and pi write nothing when a conversation is rewound, only when the
// next prompt is sent, so until then their transcript still holds the turns
// that are gone. The projection hides them for exactly that window.
type rewoundMessages struct {
	mu sync.Mutex
	at map[string]string
}

func (r *rewoundMessages) set(workerID, eventID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.at == nil {
		r.at = map[string]string{}
	}
	r.at[workerID] = eventID
}

// apply drops the rewound turns from a freshly loaded conversation. The mark is
// released once the provider's own transcript no longer contains the prompt.
func (r *rewoundMessages) apply(workerID string, conversation work.CodexConversation) work.CodexConversation {
	r.mu.Lock()
	defer r.mu.Unlock()
	eventID, held := r.at[workerID]
	if !held {
		return conversation
	}
	for index, event := range conversation.Events {
		if event.ID == eventID {
			trimmed := conversation
			trimmed.Events = append([]work.CodexConversationEvent{}, conversation.Events[:index]...)
			return trimmed
		}
	}
	delete(r.at, workerID)
	return conversation
}

func (s *Server) handleRewindMessage(conn *websocket.Conn, raw clientMessage) {
	var request rewindMessageRequest
	if len(raw.MessageRewind) == 0 || json.Unmarshal(raw.MessageRewind, &request) != nil || strings.TrimSpace(request.EventID) == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "rewind_message_invalid", "missing message")
		return
	}
	resolved := s.resolveCodexConversationWorker(raw)
	if !resolved.ready || !resolved.fromWatcher {
		s.sendErrorWithRequestID(conn, raw.RequestID, "rewind_message_refused", "This Session is not live.")
		return
	}
	reader := work.NewProviderConversationReader()
	load := func() (work.CodexConversation, error) {
		return s.loadProviderConversation(reader, resolved, time.Now())
	}
	composerClean, err := s.rewindMessage(resolved.targetID, resolved.provider, request, load)
	if err != nil {
		code := "rewind_message_failed"
		if errors.Is(err, watcher.ErrMessageRewindRefused) {
			code = "rewind_message_refused"
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, code, rewindMessageUserText(err))
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "message_rewound",
		"request_id": raw.RequestID,
		"worker_id":  resolved.targetID,
		"event_id":   request.EventID,
		// False when the CLI stepped back but its input box could not be proven
		// empty: the rewind happened, and nothing may be sent until it is cleared.
		"composer_clean": composerClean,
	})
}

func rewindMessageUserText(err error) string {
	message := strings.TrimPrefix(err.Error(), watcher.ErrMessageRewindRefused.Error()+": ")
	if message == "" {
		return "Could not rewind."
	}
	return strings.ToUpper(message[:1]) + message[1:] + "."
}

// rewindMessage steps the Session's CLI back to the prompt and leaves its
// composer empty. It refuses anything it cannot prove: a live, idle Session
// that is not Brain's own thread, and a prompt that is a real user message.
func (s *Server) rewindMessage(workerID, provider string, request rewindMessageRequest, load func() (work.CodexConversation, error)) (composerClean bool, err error) {
	rewindProvider, ok := messageRewindProvider(provider)
	if !ok {
		return false, fmt.Errorf("%w: this agent cannot rewind a conversation", watcher.ErrMessageRewindRefused)
	}
	if s.brain != nil && s.brain.CurrentHostSessionID() == workerID {
		return false, fmt.Errorf("%w: Brain's conversation is never rewound", watcher.ErrMessageRewindRefused)
	}
	conversation, err := load()
	if err != nil {
		return false, err
	}
	if conversation.Activity != nil && conversation.Activity.Status == work.ProviderActivityRunning {
		return false, fmt.Errorf("%w: the agent is working; stop it first", watcher.ErrMessageRewindRefused)
	}
	target := -1
	for index, event := range conversation.Events {
		if event.ID == request.EventID {
			target = index
			break
		}
	}
	if target < 0 || conversation.Events[target].Kind != "user_message" || conversation.Events[target].Role != "user" {
		return false, fmt.Errorf("%w: that message is no longer in the conversation", watcher.ErrMessageRewindRefused)
	}
	back := 0
	for _, event := range conversation.Events[target+1:] {
		if event.Kind == "user_message" && event.Role == "user" {
			back++
		}
	}
	rewind := watcher.MessageRewindRequest{Provider: rewindProvider, Text: conversation.Events[target].Body, Back: back}
	drive := s.rewindMessageOverride
	if drive == nil {
		drive = s.watcher.RewindMessage
	}
	driveErr := drive(workerID, rewind)
	if driveErr != nil && !errors.Is(driveErr, watcher.ErrMessageRewindInputNotClean) {
		return false, driveErr
	}
	// The CLI stepped back, so the projection hides the rewound turns either way.
	s.rewound.set(workerID, request.EventID)
	return driveErr == nil, nil
}
