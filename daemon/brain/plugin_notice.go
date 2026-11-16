package brain

import (
	"log"
	"strconv"
	"strings"
	"time"
)

// NotePluginSignInLost tells Chat, once per loss, that a plugin account
// stopped working and what fixes it. id and at identify the loss, so a
// repeated report of the same one adds no second row.
func (s *Service) NotePluginSignInLost(id string, at time.Time, title, body string) {
	threadID, err := s.store.ChatThreadID()
	if err != nil || strings.TrimSpace(threadID) == "" {
		log.Printf("plugin %s sign-in lost; no Chat thread for the notice: %v", id, err)
		return
	}
	host, _ := s.store.HostSession()
	sessionID := firstNonEmpty(host.ID, "plugins")
	if _, err := s.store.AppendTimelineItem(TimelineItem{
		ID:         "plugin-signin-lost:" + id + ":" + strconv.FormatInt(at.UnixNano(), 10),
		ThreadID:   threadID,
		SessionID:  sessionID,
		ExecutorID: host.ExecutorID,
		Role:       "assistant",
		Kind:       timelineKindPluginNotice,
		Status:     "warning",
		Title:      title,
		Body:       body,
	}); err != nil {
		log.Printf("plugin %s sign-in lost; Chat notice failed: %v", id, err)
	}
}
