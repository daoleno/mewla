package work

import (
	"strings"

	"github.com/daoleno/mewla/daemon/attachment"
)

// SanitizeConversationProjection is the final user-facing API boundary. It
// drops typed provider Goal context and Mewla's reserved direct Work Event input,
// then cleans every displayable text field even when a provider adds a new
// structured envelope around legacy context.
func SanitizeConversationProjection(conversation CodexConversation) CodexConversation {
	events := make([]CodexConversationEvent, 0, len(conversation.Events))
	for _, event := range conversation.Events {
		if isGoalInternalContextEvent(event) ||
			event.Kind == "user_message" &&
				IsDirectWorkEventPresentationInput(event.Body) {
			continue
		}
		event = ProjectUserAttachments(event)
		event.Title = CleanCodexDisplayText(event.Title)
		event.Body = CleanCodexDisplayText(event.Body)
		event.Command = CleanCodexDisplayText(event.Command)
		event.Input = CleanCodexDisplayText(event.Input)
		event.Output = CleanCodexDisplayText(event.Output)
		event.Explanation = CleanCodexDisplayText(event.Explanation)
		for index := range event.Plan {
			event.Plan[index].Step = CleanCodexDisplayText(event.Plan[index].Step)
		}
		if event.Kind == "user_message" || event.Kind == "assistant_message" {
			if event.Body == "" && len(event.Attachments) == 0 {
				continue
			}
		}
		events = append(events, event)
	}
	conversation.Events = events
	return conversation
}

func isGoalInternalContextEvent(event CodexConversationEvent) bool {
	source := strings.ToLower(strings.TrimSpace(event.Source))
	title := strings.ToLower(strings.TrimSpace(event.Title))
	toolName := strings.ToLower(strings.TrimSpace(event.ToolName))
	return source == "goal" ||
		source == "codex_internal_context" ||
		source == "codex_internal_context:goal" ||
		title == "codex_internal_context" ||
		toolName == "codex_internal_context"
}

// ProjectUserAttachments moves the files a provider echoed inside a user
// message from its text into the typed Attachments field. Providers only ever
// receive text, so this is the one place the echo is read back; text without
// the rendering, or with one that cannot be read, is left as it is (old rows
// stored before attachments were typed stay plain text).
func ProjectUserAttachments(event CodexConversationEvent) CodexConversationEvent {
	if event.Kind != "user_message" || len(event.Attachments) > 0 {
		return event
	}
	message, err := attachment.Split(event.Body)
	if err != nil {
		return event
	}
	event.Body = message.Text
	event.Attachments = message.Files
	return event
}
