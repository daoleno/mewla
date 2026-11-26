package translate

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// upstreamBody is the error object of any of the three APIs:
// {"error":{"message","type","code"}} (OpenAI) or {"type":"error",
// "error":{"type","message"}} (Anthropic).
type upstreamBody struct {
	Message string          `json:"message"`
	Type    string          `json:"type"`
	Code    json.RawMessage `json:"code"`
}

func (b *upstreamBody) toError(status int) *Error {
	code := ""
	_ = json.Unmarshal(b.Code, &code)
	e := &Error{Status: status, Message: strings.TrimSpace(b.Message)}
	if e.Message == "" {
		e.Message = firstNonEmpty(b.Type, code, http.StatusText(status), "The Provider failed.")
	}
	e.Kind = errorKind(status, b.Type+" "+code, e.Message)
	if e.Status == 0 {
		e.Status = statusOfKind(e.Kind)
	}
	return e
}

// contextWords are how APIs say a prompt is longer than the model's window.
var contextWords = regexp.MustCompile(`(?i)context.?length|context.?window|maximum context|prompt is too long|too many (input )?tokens|input is too long|exceeds? the (model'?s? )?(maximum|context)|reduce the length|context_length_exceeded|上下文长度|超出.*(上下文|长度)`)

func errorKind(status int, code, message string) string {
	if contextWords.MatchString(code) || contextWords.MatchString(message) {
		return ErrContext
	}
	switch {
	case status == http.StatusUnauthorized || strings.Contains(code, "authentication"):
		return ErrAuth
	case status == http.StatusForbidden || strings.Contains(code, "permission"):
		return ErrDenied
	case status == http.StatusNotFound || strings.Contains(code, "not_found"):
		return ErrNotFound
	case status == http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case status == http.StatusTooManyRequests || strings.Contains(code, "rate_limit"):
		return ErrRate
	case status == 529 || strings.Contains(code, "overloaded"):
		return ErrOverload
	case status >= 500 || strings.Contains(code, "api_error") || strings.Contains(code, "server_error"):
		return ErrServer
	}
	return ErrInvalid
}

func statusOfKind(kind string) int {
	switch kind {
	case ErrAuth:
		return http.StatusUnauthorized
	case ErrDenied:
		return http.StatusForbidden
	case ErrNotFound:
		return http.StatusNotFound
	case ErrTooLarge:
		return http.StatusRequestEntityTooLarge
	case ErrRate:
		return http.StatusTooManyRequests
	case ErrOverload:
		return 529
	case ErrServer:
		return http.StatusBadGateway
	}
	return http.StatusBadRequest
}

// ReadUpstreamError reads an upstream's error reply, in any API's shape or
// none.
func ReadUpstreamError(status int, body []byte) *Error {
	var wrapped struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &wrapped) == nil && len(wrapped.Error) > 0 {
		var b upstreamBody
		if json.Unmarshal(wrapped.Error, &b) == nil {
			return b.toError(status)
		}
		var s string
		if json.Unmarshal(wrapped.Error, &s) == nil {
			return (&upstreamBody{Message: s}).toError(status)
		}
	}
	var flat upstreamBody
	if json.Unmarshal(body, &flat) == nil && flat.Message != "" {
		return flat.toError(status)
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		text = text[:500]
	}
	return (&upstreamBody{Message: text}).toError(status)
}

// AnthropicError is e in Anthropic's shape, with its status. A prompt too
// long for the window is said the way Claude Code recognizes ("prompt is too
// long"), so it compacts the conversation and goes on.
func AnthropicError(e *Error) (int, []byte) {
	typ := map[string]string{
		ErrAuth: "authentication_error", ErrDenied: "permission_error", ErrNotFound: "not_found_error",
		ErrTooLarge: "request_too_large", ErrRate: "rate_limit_error", ErrOverload: "overloaded_error",
		ErrServer: "api_error", ErrContext: "invalid_request_error",
	}[e.Kind]
	if typ == "" {
		typ = "invalid_request_error"
	}
	msg := e.Message
	status := e.Status
	if e.Kind == ErrContext {
		status = http.StatusBadRequest
		if !strings.Contains(strings.ToLower(msg), "prompt is too long") {
			msg = "prompt is too long: " + msg
		}
	}
	if status == 0 {
		status = statusOfKind(e.Kind)
	}
	body, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": msg}})
	return status, body
}

// OpenAIError is e in OpenAI's shape, with its status. A prompt too long for
// the window carries code context_length_exceeded, which Codex compacts on.
func OpenAIError(e *Error) (int, []byte) {
	typ, code := "invalid_request_error", ""
	switch e.Kind {
	case ErrAuth:
		typ, code = "authentication_error", "invalid_api_key"
	case ErrDenied:
		typ = "permission_error"
	case ErrNotFound:
		typ, code = "invalid_request_error", "model_not_found"
	case ErrRate:
		typ, code = "rate_limit_error", "rate_limit_exceeded"
	case ErrOverload, ErrServer:
		typ, code = "server_error", "server_error"
	case ErrContext:
		code = "context_length_exceeded"
	}
	status := e.Status
	if e.Kind == ErrContext {
		status = http.StatusBadRequest
	}
	if status == 0 {
		status = statusOfKind(e.Kind)
	}
	inner := map[string]any{"message": e.Message, "type": typ}
	if code != "" {
		inner["code"] = code
	}
	body, _ := json.Marshal(map[string]any{"error": inner})
	return status, body
}
