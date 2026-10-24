// Package claude adapts Claude Code's hooks to the normalized agent events.
//
// Mewla launches each delegated Claude with --settings pointing at a hook file
// whose commands run `mewla agent-event claude`. Claude calls them with a JSON
// payload on stdin for the events below; Map turns one payload into the
// normalized events, and PermissionRequest is answered through Decision.
package claude

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/work"
)

// HookEvents are the hooks Mewla registers. Only these fire a process, so the
// list is the whole cost of watching a Claude Worker.
var HookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "PermissionDenied", "Stop", "StopFailure", "SessionEnd",
}

// Payload is the part of a hook payload Mewla reads. Prompt text is hashed
// and then dropped; it is never stored.
type Payload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	HookEvent      string          `json:"hook_event_name"`
	PromptID       string          `json:"prompt_id"`
	Prompt         string          `json:"prompt"`
	Source         string          `json:"source"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	// Reason is PermissionDenied's denial reason; for a classifier verdict it
	// names the matched rule in square brackets.
	Reason       string `json:"reason"`
	Error        string `json:"error"`
	ErrorDetails string `json:"error_details"`
}

// Parse decodes a hook payload.
func Parse(raw []byte) (Payload, error) {
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Payload{}, fmt.Errorf("claude hook payload: %w", err)
	}
	if strings.TrimSpace(p.HookEvent) == "" {
		return Payload{}, fmt.Errorf("claude hook payload has no hook_event_name")
	}
	return p, nil
}

// Map turns one hook payload into the normalized events it carries for the
// Worker. PermissionRequest yields none here: it is a question, answered
// through Decision.
func Map(workerID string, p Payload, now time.Time) []agentevents.Event {
	event := func(kind agentevents.EventKind) agentevents.Event {
		return agentevents.Event{WorkerID: workerID, Kind: kind, At: now, TurnID: p.PromptID}
	}
	switch p.HookEvent {
	case "SessionStart":
		bound := event(agentevents.KindBound)
		bound.TurnID = ""
		bound.Binding = &agentevents.Binding{ConversationID: p.SessionID, Ref: p.TranscriptPath, By: "agent"}
		return []agentevents.Event{bound}
	case "UserPromptSubmit":
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(p.Prompt)))
		unwrapped := digest
		// Mewla's delegated brief arrives typed-prefix plus one paste envelope;
		// its submitted digest is the brief's own.
		if brief, delegated := work.UnwrapClaudeDelegatedInput(p.Prompt); delegated {
			unwrapped = fmt.Sprintf("%x", sha256.Sum256([]byte(brief)))
		}
		msg := event(agentevents.KindUserMessage)
		msg.ID = p.PromptID
		msg.InputSHA256, msg.InputUnwrappedSHA256 = digest, unwrapped
		started := event(agentevents.KindTurnStarted)
		started.Status = agentevents.TurnRunning
		return []agentevents.Event{msg, started}
	case "PostToolUse":
		return []agentevents.Event{event(agentevents.KindProgress)}
	case "Stop":
		done := event(agentevents.KindTurnFinished)
		done.Status = agentevents.TurnCompleted
		return []agentevents.Event{done}
	case "StopFailure":
		done := event(agentevents.KindTurnFinished)
		done.Status = agentevents.TurnFailed
		done.Error = ErrorFromStopFailure(p.Error, p.ErrorDetails)
		return []agentevents.Event{done}
	case "SessionEnd":
		return []agentevents.Event{event(agentevents.KindExit)}
	}
	return nil
}

// ErrorFromStopFailure maps Claude's StopFailure error enum onto the closed
// set of error classes. Anything it does not know is "unknown".
func ErrorFromStopFailure(code, details string) *agentevents.TurnError {
	class := agentevents.ErrorUnknown
	switch strings.TrimSpace(code) {
	case "rate_limit":
		class = agentevents.ErrorRateLimit
	case "overloaded":
		class = agentevents.ErrorOverloaded
	case "authentication_failed", "oauth_org_not_allowed", "cloud_credential_error":
		class = agentevents.ErrorAuth
	case "billing_error", "account_on_hold":
		class = agentevents.ErrorBilling
	case "invalid_request", "model_not_found":
		class = agentevents.ErrorInvalidRequest
	case "max_output_tokens":
		class = agentevents.ErrorContext
	case "server_error":
		class = agentevents.ErrorServer
	}
	return &agentevents.TurnError{Class: class, Code: strings.TrimSpace(code), Message: strings.TrimSpace(details)}
}

// Approval builds the approval request a PermissionRequest hook stands for.
// Claude's hook has no request id of its own, so Mewla supplies id.
func Approval(id string, p Payload) agentevents.Approval {
	return agentevents.Approval{
		ID:             id,
		ConversationID: p.SessionID,
		Kind:           "permission",
		Tool:           p.ToolName,
		Summary:        summarizeToolInput(p.ToolName, p.ToolInput),
		Options: []agentevents.ApprovalOption{
			{ID: "allow", Label: "Allow", Scope: "once"},
			{ID: "deny", Label: "Deny", Scope: "deny"},
		},
	}
}

// AutoDenied is the request a PermissionDenied hook stands for: Auto mode
// refused the action and nothing runs unless it is approved and retried.
func AutoDenied(id string, p Payload) agentevents.Approval {
	return agentevents.Approval{
		ID:             id,
		ConversationID: p.SessionID,
		Kind:           "auto_denied",
		Tool:           p.ToolName,
		Summary:        summarizeToolInput(p.ToolName, p.ToolInput),
		Reason:         strings.TrimSpace(p.Reason),
		Options: []agentevents.ApprovalOption{
			{ID: "allow", Label: "Allow once", Scope: "once"},
			{ID: "deny", Label: "Keep blocked", Scope: "deny"},
		},
	}
}

// DeniedDecision is the PermissionDenied hook output for an answer. Approving
// cannot reverse the refusal: it tells the agent it may retry, and the retry
// passes only through ToolGrant. A refusal that stands prints nothing.
func DeniedDecision(optionID string) (output string, ok bool) {
	if optionID != "allow" {
		return "", false
	}
	raw, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionDenied",
		"retry":         true,
	}})
	return string(raw), true
}

// ToolGrant is the PreToolUse hook output that lets one approved call run past
// Auto mode's refusal.
func ToolGrant(reason string) string {
	raw, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "allow",
		"permissionDecisionReason": reason,
	}})
	return string(raw)
}

// DenyPrompt is the PermissionRequest hook output that refuses a prompt nobody
// is there to answer, so a delegated Worker is told why and moves on instead
// of waiting at a terminal prompt.
func DenyPrompt(message string) string {
	raw, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionRequest",
		"decision":      map[string]any{"behavior": "deny", "message": message},
	}})
	return string(raw)
}

// ActionKey identifies one tool call independently of fields that vary between
// attempts (a Bash call's description or timeout): the command for a shell
// call, otherwise the whole input.
func ActionKey(tool string, input json.RawMessage) string {
	var fields struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &fields)
	if strings.TrimSpace(fields.Command) != "" {
		return tool + "\x00" + strings.TrimSpace(fields.Command)
	}
	var generic any
	if err := json.Unmarshal(input, &generic); err != nil {
		return tool + "\x00" + string(input)
	}
	canonical, _ := json.Marshal(generic)
	return tool + "\x00" + string(canonical)
}

// Decision is the hook output that answers a PermissionRequest with the
// option the user chose; ok is false for an option Claude's hook cannot carry.
func Decision(optionID string) (output string, ok bool) {
	var behavior string
	switch optionID {
	case "allow":
		behavior = "allow"
	case "deny":
		behavior = "deny"
	default:
		return "", false
	}
	raw, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionRequest",
		"decision":      map[string]any{"behavior": behavior},
	}})
	return string(raw), true
}

func summarizeToolInput(tool string, input json.RawMessage) string {
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
