package brain

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

func chatTurn(kind, body string) TimelineItem {
	return TimelineItem{Kind: kind, Body: body, CreatedAt: time.Now()}
}

func TestConversationHandoffCarriesBoundedChatTail(t *testing.T) {
	var items []TimelineItem
	for i := 0; i < 100; i++ {
		items = append(items, chatTurn(timelineKindUserMessage, fmt.Sprintf("question-%03d", i)))
		items = append(items, chatTurn(timelineKindAssistantMessage, fmt.Sprintf("answer-%03d", i)))
	}
	items = append(items,
		chatTurn(timelineKindWorkCard, "work-card-body"),
		TimelineItem{Kind: timelineKindUserMessage, Origin: TimelineOriginProvider, Body: "provider-compaction-summary"},
		chatTurn(timelineKindUserMessage, "Brain Host activation contract: private"),
	)
	prompt := formatHostConversationHandoff(items)
	for _, required := range []string{"user: question-099", "Brain: answer-099", "mewla memory search", "mewla memory show", "policies/handoff.md", "pending Event identities", "earlier turns left out"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("handoff missing %q:\n%s", required, prompt)
		}
	}
	for _, excluded := range []string{"question-000", "work-card-body", "provider-compaction-summary", "Host activation contract", brainWorkerRoleContract} {
		if strings.Contains(prompt, excluded) {
			t.Fatalf("handoff leaked or kept %q", excluded)
		}
	}
	if strings.Index(prompt, "question-098") > strings.Index(prompt, "question-099") {
		t.Fatal("chat is not oldest first")
	}
	if len(prompt) > hostHandoffMaxTotalRunes*2 {
		t.Fatalf("handoff expanded to %d bytes", len(prompt))
	}
	if !work.IsPrivateHostPrompt("Brain Host activation contract:\n\n" + prompt) {
		t.Fatal("bootstrap carrying the handoff lost transcript privacy")
	}
}

func TestConversationHandoffTrimsLongTurnsAndRedactsSecrets(t *testing.T) {
	long := strings.Repeat("x", hostHandoffMaxRowRunes*3)
	prompt := formatHostConversationHandoff([]TimelineItem{
		chatTurn(timelineKindUserMessage, long),
		chatTurn(timelineKindAssistantMessage, "token sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"),
	})
	if strings.Contains(prompt, long) || !strings.Contains(prompt, "…[trimmed]") {
		t.Fatal("a long turn was not capped")
	}
	if strings.Contains(prompt, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatal("a secret reached the handoff")
	}
}

func TestConversationHandoffIsEmptyWithoutChat(t *testing.T) {
	if formatHostConversationHandoff(nil) != "" || formatHostConversationHandoff([]TimelineItem{chatTurn(timelineKindWorkCard, "x")}) != "" {
		t.Fatal("no chat produced a handoff")
	}
}
