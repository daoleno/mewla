package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// A New chat starts with nothing of the chat before it: the new Host's
// activation carries no handoff of the old thread.
func TestNewChatHostActivationCarriesNoOldChat(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "mewla-worker-brain-old:@1"
	if err := store.SetHostSession(oldID, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "brain_old"}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []TimelineItem{
		{ID: "u1", ThreadID: "brain_old", SessionID: oldID, Kind: timelineKindUserMessage, Role: "user", Body: "Remember the code word KIWI-42.", CreatedAt: time.Now().UTC()},
		{ID: "a1", ThreadID: "brain_old", SessionID: oldID, Kind: timelineKindAssistantMessage, Role: "assistant", Body: "noted.", CreatedAt: time.Now().UTC()},
	} {
		if _, err := store.AppendTimelineItem(item); err != nil {
			t.Fatal(err)
		}
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{
		oldID: {ID: oldID, Name: "Brain", Command: "claude", State: classifier.StateRunning, Hidden: true},
	}}
	service := newTestService(t, store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"claude": {Name: "claude", Command: "claude", Kind: "claude"}},
	})
	if strings.Contains(service.hostBootstrapPrompt(service.hostExecutor()), "KIWI-42") == false {
		t.Fatal("the old chat is the handoff before New chat (fixture check)")
	}
	if _, err := service.NewChat(); err != nil {
		t.Fatal(err)
	}
	if thread, _ := store.ChatThreadID(); thread == "brain_old" || thread == "" {
		t.Fatalf("New chat thread = %q", thread)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created = %+v", fw.created)
	}
	newID := fw.created[0].id
	texts := []string{fw.created[0].opts.Command}
	for _, sent := range fw.sentCalls {
		texts = append(texts, sent.text)
	}
	service.hostActivationMu.Lock()
	texts = append(texts, service.hostBlank[newID])
	service.hostActivationMu.Unlock()
	for _, text := range texts {
		if strings.Contains(text, "KIWI-42") || strings.Contains(text, "Conversation handoff") {
			t.Fatalf("the New chat's Host got the old chat: %q", text)
		}
	}
}
