package brain

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

func (s *Service) ChatThreadID() (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.ChatThreadID()
}

func (s *Service) HasChatThread(threadID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	return s.store.HasChatThread(threadID)
}

// ConversationProjectionVersion is the Store's change counter: it moves
// whenever a write could change a thread conversation projection.
func (s *Service) ConversationProjectionVersion() uint64 {
	if s == nil || s.store == nil {
		return 0
	}
	return s.store.Version()
}

func (s *Service) ChatThreadIDs() ([]string, error) {
	if s == nil || s.store == nil {
		return []string{}, nil
	}
	return s.store.ChatThreadIDs()
}

func (s *Service) NewChat() (Snapshot, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return Snapshot{}, fmt.Errorf("brain service is not configured")
	}
	executor := s.hostExecutor()
	// A New chat fails only when no agent at all can run Brain: then nothing
	// changes and the old chat stays current. When Brain's agent cannot run
	// but another installed one can, the person still gets the new chat and
	// the Host comes up on that agent (the default pick, below).
	if err := s.hostRunnable(executor); err != nil {
		if _, ok := s.installedHostExecutor(); !ok {
			return Snapshot{}, err
		}
	}
	if hostSession, err := s.store.HostSession(); err == nil {
		if id := strings.TrimSpace(hostSession.ID); id != "" {
			// Explicit NewChat destroys the host; release any managed route.
			if err := s.teardownHostSession(id); err != nil {
				return Snapshot{}, err
			}
		}
	} else {
		return Snapshot{}, err
	}
	if err := s.store.SetHostSession("", executor.ID); err != nil {
		return Snapshot{}, err
	}
	// The new chat is current before its Host starts: the Host's activation
	// hands over the current chat, and a New chat hands over nothing.
	threadID := newChatThreadID()
	if err := s.store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		return Snapshot{}, err
	}
	if _, err := s.ensureHostWorker(executor); err != nil {
		if !errors.Is(err, ErrHostNotRunnable) {
			return Snapshot{}, err
		}
		// The new chat is current; its Host is started by Host continuity on
		// an agent that can run, and shows as coming up until then.
		log.Printf("brain new chat: %v; the Host starts on another installed agent", err)
		s.noteHostChosen()
	}
	return s.Snapshot()
}

func (s *Service) nowUTC() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}
