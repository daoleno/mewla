package work

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/attachment"
)

func TestSanitizeMovesRenderedAttachmentsIntoTypedFields(t *testing.T) {
	files := []attachment.File{{Name: "photo.png", Path: "/uploads/photo.png", ContentType: "image/png"}}
	rendered := attachment.Message{Text: "look at this", Files: files}.Render()
	only := attachment.Message{Files: files}.Render()
	conversation := SanitizeConversationProjection(CodexConversation{Events: []CodexConversationEvent{
		{ID: "u1", Kind: "user_message", Body: rendered},
		{ID: "u2", Kind: "user_message", Body: only},
		{ID: "u3", Kind: "user_message", Body: "plain\n\nUploaded files:\n- a.png: /uploads/a.png"},
		{ID: "u4", Kind: "user_message", Body: "<mewla_attachments>{broken</mewla_attachments>"},
		{ID: "a1", Kind: "assistant_message", Body: rendered},
	}})
	byID := map[string]CodexConversationEvent{}
	for _, event := range conversation.Events {
		byID[event.ID] = event
	}
	if got := byID["u1"]; got.Body != "look at this" || len(got.Attachments) != 1 || got.Attachments[0].Path != "/uploads/photo.png" || got.Attachments[0].ContentType != "image/png" {
		t.Fatalf("u1 = %+v", got)
	}
	if got := byID["u2"]; got.Body != "" || len(got.Attachments) != 1 {
		t.Fatalf("attachment-only message was dropped or kept text: %+v", got)
	}
	if got := byID["u3"]; got.Body != "plain\n\nUploaded files:\n- a.png: /uploads/a.png" || len(got.Attachments) != 0 {
		t.Fatalf("legacy text list must stay plain text: %+v", got)
	}
	if got := byID["u4"]; !strings.Contains(got.Body, "{broken") || len(got.Attachments) != 0 {
		t.Fatalf("unreadable rendering must stay text: %+v", got)
	}
	if got := byID["a1"]; got.Body != rendered || len(got.Attachments) != 0 {
		t.Fatalf("only user messages carry attachments: %+v", got)
	}
}
