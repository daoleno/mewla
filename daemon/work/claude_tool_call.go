package work

import (
	"encoding/json"
	"strings"
)

// ClaudeToolSummary is the one line a tool call is shown as in a request card:
// the command, else the file, else the tool's own description, else the tool's
// name. The permission hook's approval and the transcript's pending call both
// use it, so a Session with the hook and one without word a request the same.
func ClaudeToolSummary(tool string, input json.RawMessage) string {
	var fields struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		FilePath    string `json:"file_path"`
	}
	_ = json.Unmarshal(input, &fields)
	switch {
	case fields.Command != "":
		return fields.Command
	case fields.FilePath != "":
		return fields.FilePath
	case fields.Description != "":
		return fields.Description
	}
	return tool
}

// ConversationToolCall is a tool call of a conversation: its provider call id,
// the tool's name and the line a request card shows for it.
type ConversationToolCall struct {
	CallID  string
	Tool    string
	Summary string
	// Done is true once the call has a result; Rejected says that result is
	// the person (or a permission rule) refusing the call, not the tool
	// failing; Error is any other failed result.
	Done     bool
	Rejected bool
	Error    bool
}

// conversationToolCall reads a tool call out of one conversation event, or
// ok=false for an event that is not one (a message, a question to the person).
func conversationToolCall(event CodexConversationEvent) (ConversationToolCall, bool) {
	callID := strings.TrimSpace(event.CallID)
	if callID == "" || event.Choice != nil {
		return ConversationToolCall{}, false
	}
	call := ConversationToolCall{CallID: callID, Done: event.Status != "running"}
	switch event.Kind {
	case "command":
		call.Tool = "Bash"
		call.Summary = strings.TrimSpace(event.Command)
	case "tool":
		call.Tool = strings.TrimSpace(event.ToolName)
		call.Summary = ClaudeToolSummary(call.Tool, json.RawMessage(event.Input))
	default:
		return ConversationToolCall{}, false
	}
	if call.Tool == "" || call.Tool == "tool" || call.Summary == "" {
		return ConversationToolCall{}, false
	}
	if call.Done {
		if event.Rejected {
			call.Rejected = true
		} else if event.Status == "failed" {
			call.Error = true
		}
	}
	return call, true
}

// PendingConversationToolCalls are the tool calls with no result yet, newest
// first. A permission prompt open in the terminal asks about one of them.
func PendingConversationToolCalls(conversation CodexConversation) []ConversationToolCall {
	var calls []ConversationToolCall
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		if call, ok := conversationToolCall(conversation.Events[index]); ok && !call.Done {
			calls = append(calls, call)
		}
	}
	return calls
}

// ConversationToolCallByID is the tool call with the provider call id, with
// its result when it has one.
func ConversationToolCallByID(conversation CodexConversation, callID string) (ConversationToolCall, bool) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return ConversationToolCall{}, false
	}
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		if call, ok := conversationToolCall(conversation.Events[index]); ok && call.CallID == callID {
			return call, true
		}
	}
	return ConversationToolCall{}, false
}

// ConversationToolCallByLine is the newest tool call with the tool's name and
// the line a request card showed for it.
func ConversationToolCallByLine(conversation CodexConversation, tool, summary string) (ConversationToolCall, bool) {
	tool, summary = strings.TrimSpace(tool), strings.Join(strings.Fields(summary), " ")
	if tool == "" || summary == "" {
		return ConversationToolCall{}, false
	}
	for index := len(conversation.Events) - 1; index >= 0; index-- {
		call, ok := conversationToolCall(conversation.Events[index])
		if ok && call.Tool == tool && strings.Join(strings.Fields(call.Summary), " ") == summary {
			return call, true
		}
	}
	return ConversationToolCall{}, false
}
