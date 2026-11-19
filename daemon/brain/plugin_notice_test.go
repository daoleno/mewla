package brain

import (
	"strings"
	"testing"
	"time"
)

func TestPluginSignInLostShowsInChatOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	at := time.Date(2026, 11, 16, 8, 40, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		service.NotePluginSignInLost("a14f", at, "Slack sign-in ended", "Slack stopped accepting “Team”. Reconnect it in Extensions.")
	}
	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, defaultThreadTimelineLimit)
	if err != nil {
		t.Fatal(err)
	}
	notices := 0
	for _, item := range items {
		if item.Kind == timelineKindPluginNotice {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("plugin notices = %d in %+v", notices, items)
	}
	shown := false
	for _, event := range TimelineItemsToConversationEvents(items) {
		if event.Source == timelineKindPluginNotice {
			// The row names its account, so its Sign in renews that account.
			shown = event.Kind == "status" && event.Title == "Slack sign-in ended" && event.Status == "warning" && event.PluginAccountID == "a14f"
		}
	}
	if !shown {
		t.Fatal("Chat does not show the plugin notice")
	}
}

// The agents' browser losing sign-ins shows in Chat as one status row naming
// the sites, once per loss.
func TestAgentBrowserSignInsLostShowsInChatOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	at := time.Date(2026, 11, 18, 11, 52, 40, 0, time.UTC)
	for i := 0; i < 2; i++ {
		service.NoteAgentBrowserSignInsLost([]string{"accounts.google.com", "github.com"}, at)
	}
	service.NoteAgentBrowserSignInsLost(nil, at.Add(time.Minute))
	threadID, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, defaultThreadTimelineLimit)
	if err != nil {
		t.Fatal(err)
	}
	var shown []string
	for _, event := range TimelineItemsToConversationEvents(items) {
		if event.Source == timelineKindBrowserNotice && event.Kind == "status" && event.Status == "warning" {
			shown = append(shown, event.Body)
		}
	}
	if len(shown) != 1 || !strings.Contains(shown[0], "accounts.google.com, github.com") {
		t.Fatalf("Chat shows %q", shown)
	}
}
