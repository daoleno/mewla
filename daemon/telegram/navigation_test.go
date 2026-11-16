package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
)

func navigationRow(t *testing.T, m *Manager, id string) outboxRecord {
	t.Helper()
	for _, row := range m.store.snapshot().Outbox {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("missing reply %s", id)
	return outboxRecord{}
}

func navigationCallback(id, threadID int64, data string) Update {
	return Update{UpdateID: id, CallbackQuery: &CallbackQuery{ID: fmt.Sprint(id), Data: data,
		From: &User{ID: 10}, Message: &Message{MessageID: 55, MessageThreadID: threadID, Chat: Chat{ID: 10, Type: "private"}}}}
}

func assertTopicButton(t *testing.T, button InlineKeyboardButton, topic int64) {
	t.Helper()
	want := fmt.Sprintf("https://t.me/fixture_bot/%d?thread=%d", topic, topic)
	if button.URL != want || button.CallbackData != "" {
		t.Fatalf("topic navigation is not an exact URL-only button: %+v, want %s", button, want)
	}
	encoded, _ := json.Marshal(button)
	if strings.Contains(string(encoded), "callback_data") {
		t.Fatal("Telegram requires exactly one button action")
	}
}

func TestTopicChooserAndBackStayLocalAndUseExactLinks(t *testing.T) {
	m, _, service, _, api, root := realConversationFixture(t)
	if err := m.ensureBrainTopic(); err != nil {
		t.Fatal(err)
	}
	if err := m.projectSessionTopics(t.Context(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	for m.hasDeliverableTopicOp() {
		if err := m.deliverTopicOpOne(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	a, b := topicThreadFor(t, m, "session-a"), topicThreadFor(t, m, "session-b")
	primary := m.store.snapshot().BrainTopicID
	choicesByRow := map[string][]string{}
	for _, update := range []Update{topicUpdate(2, a, "/sessions"), navigationCallback(3, a, "sessions"), topicUpdate(4, b, "/brain"), navigationCallback(5, a, "brain")} {
		if err := m.handleUpdate(t.Context(), "fixture-token", update); err != nil {
			t.Fatal(err)
		}
		choicesByRow[fmt.Sprintf("command:%d:0", update.UpdateID)] = m.store.snapshot().SessionChoices
	}
	for _, id := range []string{"command:2:0", "command:3:0"} {
		row := navigationRow(t, m, id)
		if row.MessageThreadID != a || row.ReplyMessageID == 0 {
			t.Fatalf("chooser escaped source topic: %+v", row)
		}
		// Each message owns the chooser order captured when it was rendered.
		// A later inventory can legitimately have a different display order.
		choices := choicesByRow[id]
		for i, sessionID := range choices {
			assertTopicButton(t, row.ReplyMarkup.InlineKeyboard[i][0], topicThreadFor(t, m, sessionID))
		}
	}
	for id, thread := range map[string]int64{"command:4:0": b, "callback:5:0": a} {
		row := navigationRow(t, m, id)
		if row.MessageThreadID != thread {
			t.Fatal("Back to Brain response escaped source topic")
		}
		assertTopicButton(t, row.ReplyMarkup.InlineKeyboard[0][0], primary)
		if len(row.ReplyMarkup.InlineKeyboard) != 1 {
			t.Fatal("Session navigation offered Brain New Chat")
		}
	}
	reopened, err := NewManagerWithOptions(root, service, Options{API: api})
	if err != nil {
		t.Fatal(err)
	}
	assertTopicButton(t, navigationRow(t, reopened, "command:4:0").ReplyMarkup.InlineKeyboard[0][0], primary)
	thread, _ := service.ChatThreadID()
	if thread != "brain-current" {
		t.Fatal("navigation reset canonical Brain")
	}
}

func TestFallbackStatusKeepsUnavailableRecipientAndFailedTopicHealth(t *testing.T) {
	m, owner, _, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	owner.projections = map[string]brain.SessionProjection{"gone": {SessionID: "gone"}}
	if err := m.store.mutate(func(s *durableState) error {
		s.TopicsAvailable, s.FallbackSessionID = false, "gone"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := m.ownerStatusText(); !strings.Contains(got, "gone (unavailable)") {
		t.Fatalf("wrong local status: %s", got)
	}
	if status := m.Status(); status.RecipientID != "gone" || !strings.Contains(status.RecipientLabel, "unavailable") {
		t.Fatalf("Settings lost unavailable recipient: %+v", status)
	}
	if err := m.store.mutate(func(s *durableState) error {
		s.TopicsAvailable = true
		s.TopicOps = []topicOpRecord{{ID: "fixture", State: "failed"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if status := m.Status(); status.State != StateDegraded || status.RecipientID != "" || status.RecipientLabel != "" {
		t.Fatalf("Settings falsely healthy or retained inactive fallback: %+v", status)
	}
}

func TestGeneralCommandsDoNotReplyInLastNativeBrainTopic(t *testing.T) {
	m, _, _, _ := configuredManager(t)
	bindOwner(t, m, 1, 10, 10)
	if err := m.store.mutate(func(s *durableState) error {
		s.BrainTopicID, s.BrainReplyTopicID = 101, 102
		s.BrainTopics = []int64{101, 102}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i, command := range []string{"/start", "/status", "/sessions", "/brain"} {
		id := int64(i + 2)
		if err := m.handleUpdate(t.Context(), "token", topicUpdate(id, 0, command)); err != nil {
			t.Fatal(err)
		}
		if row := navigationRow(t, m, fmt.Sprintf("command:%d:0", id)); row.MessageThreadID != 0 || row.ReplyMessageID != id {
			t.Fatalf("command replied across topics: %+v", row)
		}
	}
}
