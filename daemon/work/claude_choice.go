package work

import (
	"encoding/json"
	"strings"
)

// Claude AskUserQuestion is a blocking provider choice. Claude writes the
// tool_use (with the full questions) before the TUI shows the prompt, and the
// answer arrives later as a tool_result whose record carries
// toolUseResult.answers keyed by question text. The choice is parsed from the
// raw input before any body truncation so a long preview never cuts the JSON.
const (
	ClaudeAskUserQuestionTool = "AskUserQuestion"

	ConversationChoicePending  = "pending"
	ConversationChoiceAnswered = "answered"
	ConversationChoiceDeclined = "declined"
	// Unanswered: the conversation moved on (interrupt, restart) without a
	// result, so the prompt can no longer be on screen.
	ConversationChoiceUnanswered = "unanswered"

	maxConversationChoiceText    = 1000
	maxConversationChoicePreview = 2000
)

// ConversationChoice is the structured projection of one provider choice
// prompt. Answers align with Questions by index once State is answered.
type ConversationChoice struct {
	Kind      string                       `json:"kind"`
	State     string                       `json:"state"`
	Questions []ConversationChoiceQuestion `json:"questions"`
	Answers   []string                     `json:"answers,omitempty"`
}

type ConversationChoiceQuestion struct {
	Question    string                     `json:"question"`
	Header      string                     `json:"header,omitempty"`
	MultiSelect bool                       `json:"multi_select,omitempty"`
	Options     []ConversationChoiceOption `json:"options"`
}

type ConversationChoiceOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

// HasPreview reports Claude's side-by-side preview layout, which offers no
// free-text "Type something" row.
func (q ConversationChoiceQuestion) HasPreview() bool {
	for _, option := range q.Options {
		if option.Preview != "" {
			return true
		}
	}
	return false
}

// AllowsOther reports whether Claude renders the free-text row for this question.
func (q ConversationChoiceQuestion) AllowsOther() bool {
	return !q.HasPreview()
}

func parseClaudeAskUserQuestion(raw json.RawMessage) *ConversationChoice {
	var input struct {
		Questions []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
				Preview     string `json:"preview"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(raw, &input) != nil || len(input.Questions) == 0 {
		return nil
	}
	choice := &ConversationChoice{
		Kind:  ClaudeAskUserQuestionTool,
		State: ConversationChoicePending,
	}
	for _, question := range input.Questions {
		text := strings.TrimSpace(question.Question)
		if text == "" || len(question.Options) == 0 {
			return nil
		}
		parsed := ConversationChoiceQuestion{
			Question:    truncateRunes(text, maxConversationChoiceText),
			Header:      truncateRunes(strings.TrimSpace(question.Header), 120),
			MultiSelect: question.MultiSelect,
		}
		for _, option := range question.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" {
				return nil
			}
			parsed.Options = append(parsed.Options, ConversationChoiceOption{
				Label:       truncateRunes(label, maxConversationChoiceText),
				Description: truncateRunes(strings.TrimSpace(option.Description), maxConversationChoiceText),
				Preview:     truncateRunes(option.Preview, maxConversationChoicePreview),
			})
		}
		choice.Questions = append(choice.Questions, parsed)
	}
	return choice
}

// settleClaudeChoice applies the tool_result record to a pending choice.
// Answers come only from the structured toolUseResult; the human-readable
// tool_result text differs across Claude Code versions.
func settleClaudeChoice(choice *ConversationChoice, toolUseResult json.RawMessage, isError bool) {
	if choice == nil {
		return
	}
	if isError {
		choice.State = ConversationChoiceDeclined
		choice.Answers = nil
		return
	}
	var result struct {
		Answers map[string]string `json:"answers"`
	}
	_ = json.Unmarshal(toolUseResult, &result)
	choice.State = ConversationChoiceAnswered
	choice.Answers = make([]string, len(choice.Questions))
	for index, question := range choice.Questions {
		choice.Answers[index] = truncateRunes(result.Answers[question.Question], maxConversationChoiceText)
	}
}

// abandonPendingChoices settles every still-pending choice as unanswered once
// a visible message follows it: Claude cannot be showing that prompt anymore.
func (b *claudeConversationBuilder) abandonPendingChoices() {
	for _, callID := range b.pendingChoiceCalls {
		eventIndex, exists := b.eventByCall[callID]
		if !exists || eventIndex < 0 || eventIndex >= len(b.events) {
			continue
		}
		if choice := b.events[eventIndex].Choice; choice != nil && choice.State == ConversationChoicePending {
			abandoned := *choice
			abandoned.State = ConversationChoiceUnanswered
			b.events[eventIndex].Choice = &abandoned
		}
	}
	b.pendingChoiceCalls = nil
}

// claudeToolUseResults maps tool_use ids to the record-level toolUseResult.
// Claude stores one toolUseResult per user record, so it belongs to that
// record's single tool_result block.
func claudeToolUseResults(raw json.RawMessage, toolUseResult json.RawMessage) map[string]json.RawMessage {
	if len(toolUseResult) == 0 {
		return nil
	}
	var items []claudeContentBlock
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var ids []string
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Type), "tool_result") {
			ids = append(ids, strings.TrimSpace(item.ToolUseID))
		}
	}
	if len(ids) != 1 || ids[0] == "" {
		return nil
	}
	return map[string]json.RawMessage{ids[0]: toolUseResult}
}

// PendingConversationChoice returns the choice event for callID when it is
// still the latest pending provider choice in the conversation.
func PendingConversationChoice(conversation CodexConversation, callID string) (CodexConversationEvent, bool) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return CodexConversationEvent{}, false
	}
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		event := conversation.Events[index]
		if event.Choice == nil {
			continue
		}
		// Only the newest choice can be on screen; an older unmatched one is stale.
		if event.CallID != callID || event.Choice.State != ConversationChoicePending {
			return CodexConversationEvent{}, false
		}
		return event, true
	}
	return CodexConversationEvent{}, false
}
