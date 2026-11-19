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
		// The row's Sign in renews this account.
		PluginAccountID: id,
	}); err != nil {
		log.Printf("plugin %s sign-in lost; Chat notice failed: %v", id, err)
	}
}

// NoteAgentBrowserSignInsLost tells Chat that the agents' browser lost the
// person's sign-ins to sites, so they sign in again once instead of each
// Worker finding a site signed out and asking.
func (s *Service) NoteAgentBrowserSignInsLost(sites []string, at time.Time) {
	if len(sites) == 0 {
		return
	}
	threadID, err := s.store.ChatThreadID()
	if err != nil || strings.TrimSpace(threadID) == "" {
		log.Printf("agent browser sign-ins lost; no Chat thread for the notice: %v", err)
		return
	}
	host, _ := s.store.HostSession()
	if _, err := s.store.AppendTimelineItem(TimelineItem{
		ID:         "browser-signins-lost:" + strconv.FormatInt(at.UnixNano(), 10),
		ThreadID:   threadID,
		SessionID:  firstNonEmpty(host.ID, "browser"),
		ExecutorID: host.ExecutorID,
		Role:       "assistant",
		Kind:       timelineKindBrowserNotice,
		Status:     "warning",
		Title:      "Agents' browser signed out",
		Body:       agentBrowserSignInsLostBody(sites),
	}); err != nil {
		log.Printf("agent browser sign-ins lost; Chat notice failed: %v", err)
	}
}

func agentBrowserSignInsLostBody(sites []string) string {
	return "The agents' browser no longer has your sign-in for " + strings.Join(sites, ", ") +
		". To sign in again, take over an agent's screen in Screen."
}
