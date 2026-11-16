package telegram

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

func chromeWire(t *testing.T, m *Manager) *fakeBotAPIServer {
	t.Helper()
	fake := newFakeBotAPIServer(t)
	m.api = NewClient(fake.server.URL, fake.server.Client())
	if err := m.store.replaceToken("fixture-token"); err != nil {
		t.Fatal(err)
	}
	return fake
}

func deliverChrome(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.deliverPending(t.Context(), maxOutboxRows); err != nil {
		t.Fatal(err)
	}
}

func assertContentOnlyPayloads(t *testing.T, wire *fakeBotAPIServer) {
	t.Helper()
	if len(wire.bodies) == 0 {
		t.Fatal("no Bot API payloads")
	}
	for i, body := range wire.bodies {
		if wire.methods[i] != "sendMessage" && wire.methods[i] != "editMessageText" {
			t.Fatalf("unexpected method %s", wire.methods[i])
		}
		if _, found := body["reply_markup"]; found {
			t.Fatalf("ordinary %s contains reply markup: %+v", wire.methods[i], body)
		}
	}
}

func TestOrdinaryBrainAndSessionPayloadsStayCleanAcrossStreamChunks(t *testing.T) {
	for _, recipient := range []string{"Brain", "Session", "private Session"} {
		t.Run(recipient, func(t *testing.T) {
			m, owner, _, _ := configuredManager(t)
			bindOwner(t, m, 1, 10, 10)
			thread := int64(76315)
			if recipient == "Session" {
				thread = 102
			} else if recipient == "private Session" {
				thread = 0
			}
			if err := m.store.mutate(func(s *durableState) error {
				s.BrainTopicID, s.BrainTopics = 76315, []int64{76315}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			deliverChrome(t, m)
			wire := chromeWire(t, m)
			created := m.now().Add(time.Second)
			for i, text := range []string{"Plain answer", strings.Repeat("x", maxMessageText) + "stream", strings.Repeat("x", maxMessageText) + "stream complete"} {
				var err error
				if recipient == "Brain" {
					owner.timeline = []brain.TimelineItem{{ID: "answer", ThreadID: owner.threadID, Role: "assistant", Body: text, CreatedAt: created}}
					err = m.projectTimeline()
				} else {
					err = m.projectSessionOutput(topicMapping{SessionID: "session-a", MessageThreadID: thread}, brain.SessionProjection{
						Assistant: []brain.SessionAssistantItem{{ID: "answer", Body: text, CreatedAt: created, Partial: i < 2}},
					}, created)
				}
				if err != nil {
					t.Fatal(err)
				}
				deliverChrome(t, m)
			}
			assertContentOnlyPayloads(t, wire)
			if got := strings.Join(wire.methods, ","); got != "sendMessage,editMessageText,sendMessage,editMessageText" {
				t.Fatalf("stream did not send/edit exact chunks: %s", got)
			}
			for i, body := range wire.bodies {
				if wire.methods[i] == "sendMessage" && thread != 0 && body["message_thread_id"] != float64(thread) {
					t.Fatalf("wrong destination: %+v", body)
				}
			}
		})
	}
}

func TestExplicitInteractionPayloadsRetainExactTopicButtons(t *testing.T) {
	m, owner, _, _ := topicFixture(t)
	createTopicFor(t, m)
	if err := m.store.mutate(func(s *durableState) error {
		s.BotUsername, s.BrainTopicID, s.BrainTopics = "fixture_bot", 76315, []int64{76315}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deliverChrome(t, m)
	wire := chromeWire(t, m)
	thread := topicThreadFor(t, m, "sess-a")
	m.enqueueSessionList(10, 10, thread)
	m.returnToBrain("command:11", thread, 11)
	if got := m.handleCallback(t.Context(), "fixture-token", *navigationCallback(12, 76315, "new").CallbackQuery, 12); got != "callback_confirm" {
		t.Fatal(got)
	}
	for i, id := range []string{"command:10:0", "command:11:0", "callback:12"} {
		row := navigationRow(t, m, id)
		if err := m.deliverOne(t.Context()); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(row.ReplyMarkup)
		var expected any
		_ = json.Unmarshal(encoded, &expected)
		if row.ReplyMarkup == nil || !reflect.DeepEqual(wire.bodies[i]["reply_markup"], expected) || wire.bodies[i]["message_thread_id"] != float64(row.MessageThreadID) {
			t.Fatalf("explicit interaction changed on wire: %+v", wire.bodies[i])
		}
	}
	assertTopicButton(t, navigationRow(t, m, "command:10:0").ReplyMarkup.InlineKeyboard[0][0], thread)
	assertTopicButton(t, navigationRow(t, m, "command:11:0").ReplyMarkup.InlineKeyboard[0][0], 76315)
	if got := navigationRow(t, m, "callback:12").ReplyMarkup.InlineKeyboard; !reflect.DeepEqual(got, [][]InlineKeyboardButton{{{Text: "New Chat", CallbackData: "new_confirm"}, {Text: "Cancel", CallbackData: "brain"}}}) {
		t.Fatalf("reset confirmation changed: %+v", got)
	}
	if owner.newChats != 0 || len(owner.bodies)+len(owner.sessionBodies) != 0 {
		t.Fatal("navigation invoked a provider/reset")
	}
}
