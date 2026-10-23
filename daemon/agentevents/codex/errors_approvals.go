package codex

import (
	"encoding/json"
	"strings"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// ErrorFrom classifies a Codex error record by its codexErrorInfo variant and
// HTTP status, never by message text.
func ErrorFrom(raw json.RawMessage, willRetry bool) *agentevents.TurnError {
	failure := &agentevents.TurnError{Class: agentevents.ErrorUnknown, WillRetry: willRetry}
	var record struct {
		Message string          `json:"message"`
		Info    json.RawMessage `json:"codexErrorInfo"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &record) != nil {
		return failure
	}
	failure.Message = strings.TrimSpace(record.Message)
	name, status := errorVariant(record.Info)
	failure.Code, failure.HTTPStatus = name, status
	failure.Class = classOf(name, status)
	return failure
}

// errorVariant reads the variant name of codexErrorInfo (a bare string, or an
// object with one key) and the HTTP status the variant carries.
func errorVariant(raw json.RawMessage) (name string, status int) {
	var bare string
	if json.Unmarshal(raw, &bare) == nil {
		return bare, 0
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return "", 0
	}
	for key, value := range object {
		var detail struct {
			HTTPStatusCode *int `json:"httpStatusCode"`
		}
		if json.Unmarshal(value, &detail) == nil && detail.HTTPStatusCode != nil {
			status = *detail.HTTPStatusCode
		}
		return key, status
	}
	return "", 0
}

func classOf(variant string, status int) agentevents.ErrorClass {
	switch variant {
	case "contextWindowExceeded":
		return agentevents.ErrorContext
	case "usageLimitExceeded", "sessionBudgetExceeded":
		return agentevents.ErrorBilling
	case "rateLimitExceeded":
		return agentevents.ErrorRateLimit
	case "serverOverloaded", "flexUnavailable":
		return agentevents.ErrorOverloaded
	case "unauthorized":
		return agentevents.ErrorAuth
	case "badRequest":
		return agentevents.ErrorInvalidRequest
	case "sandboxError":
		return agentevents.ErrorSandbox
	case "internalServerError":
		return agentevents.ErrorServer
	case "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts":
		switch {
		case status == 401 || status == 403:
			return agentevents.ErrorAuth
		case status == 402:
			return agentevents.ErrorBilling
		case status == 429:
			return agentevents.ErrorRateLimit
		case status == 529:
			return agentevents.ErrorOverloaded
		case status >= 500:
			return agentevents.ErrorServer
		case status >= 400:
			return agentevents.ErrorInvalidRequest
		}
		return agentevents.ErrorNetwork
	}
	return agentevents.ErrorUnknown
}

// RequestKey is the string identity of a server request id, which is a number
// or a string on the wire.
func RequestKey(id json.RawMessage) string {
	return strings.Trim(strings.TrimSpace(string(id)), `"`)
}

// Approval maps a server request that asks the client to decide, and
// remembers what each offered option answers. ok is false for a request this
// adapter does not carry, which the TUI then decides alone.
func (m *Mapper) Approval(id json.RawMessage, method string, params json.RawMessage) (agentevents.Approval, bool) {
	var p struct {
		ThreadID           string            `json:"threadId"`
		Command            string            `json:"command"`
		Reason             string            `json:"reason"`
		GrantRoot          string            `json:"grantRoot"`
		AvailableDecisions []json.RawMessage `json:"availableDecisions"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread || m.thread == "" {
		return agentevents.Approval{}, false
	}
	approval := agentevents.Approval{ID: RequestKey(id), ConversationID: m.thread}
	switch method {
	case "item/commandExecution/requestApproval":
		approval.Kind, approval.Tool = "command", "shell"
		approval.Summary = firstNonEmpty(p.Command, p.Reason)
	case "item/fileChange/requestApproval":
		approval.Kind, approval.Tool = "file_change", "apply_patch"
		approval.Summary = firstNonEmpty(p.Reason, p.GrantRoot, "Change files")
	default:
		return agentevents.Approval{}, false
	}
	offered := []string{"accept", "acceptForSession", "decline", "cancel"}
	if len(p.AvailableDecisions) > 0 {
		offered = offered[:0]
		for _, raw := range p.AvailableDecisions {
			// Only plain decisions: the structured ones (an execution-policy
			// amendment) carry payloads Mewla does not compose.
			var name string
			if json.Unmarshal(raw, &name) == nil {
				offered = append(offered, name)
			}
		}
	}
	values := map[string]any{}
	for _, name := range offered {
		option, ok := optionFor(name)
		if !ok {
			continue
		}
		approval.Options = append(approval.Options, option)
		values[name] = name
	}
	if len(approval.Options) == 0 {
		return agentevents.Approval{}, false
	}
	m.decisions[approval.ID] = values
	return approval, true
}

func optionFor(decision string) (agentevents.ApprovalOption, bool) {
	switch decision {
	case "accept":
		return agentevents.ApprovalOption{ID: decision, Label: "Allow", Scope: "once"}, true
	case "acceptForSession":
		return agentevents.ApprovalOption{ID: decision, Label: "Allow for this session", Scope: "session"}, true
	case "decline":
		return agentevents.ApprovalOption{ID: decision, Label: "Deny", Scope: "deny"}, true
	case "cancel":
		return agentevents.ApprovalOption{ID: decision, Label: "Deny and stop the turn", Scope: "deny"}, true
	}
	return agentevents.ApprovalOption{}, false
}

// Decision is the response that answers approval id with option; ok is false
// for an option the request did not offer.
func (m *Mapper) Decision(id, option string) (response map[string]any, ok bool) {
	value, ok := m.decisions[id][option]
	if !ok {
		return nil, false
	}
	delete(m.decisions, id)
	return map[string]any{"decision": value}, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
