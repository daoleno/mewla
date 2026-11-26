// Package translate carries a model request from the API an agent speaks to
// the API its Provider speaks, and the reply back. Claude Code speaks
// Anthropic Messages and Codex speaks OpenAI Responses; a Provider may speak
// either, or only OpenAI Chat Completions. Every request is read into one
// neutral shape (Request), written out in the Provider's API, and the
// Provider's streamed reply is read as a sequence of Events that an encoder
// writes in the agent's API.
//
// The upstream is always asked to stream; an agent that asked for a whole
// reply gets the events gathered into one.
package translate

import "encoding/json"

// The APIs a request can be read from or written in.
const (
	APIAnthropic = "anthropic"
	APIResponses = "responses"
	APIChat      = "chat"
)

// Request is a model request in no API's shape.
type Request struct {
	Model    string
	System   []Part
	Messages []Message
	Tools    []Tool
	Choice   ToolChoice
	// MaxTokens is the output cap; 0 leaves it to the encoder's default.
	MaxTokens   int
	Temperature *float64
	TopP        *float64
	Stop        []string
	// Stream is whether the agent wants a stream back.
	Stream bool
	// Effort is the reasoning effort the agent asked for: none, minimal,
	// low, medium, high, xhigh or max; "" is the model's default.
	Effort string
	// ThinkingBudget is an Anthropic budget_tokens the agent gave, if any.
	ThinkingBudget int
	// Thinking is whether the agent turned thinking on (Anthropic) or asked
	// for reasoning (Responses).
	Thinking bool
	// Parallel is false when the agent asked for one tool call at a time.
	Parallel *bool
	// CacheKey is the agent's prompt cache key, passed to APIs that take
	// one.
	CacheKey string
	// User is the agent's end-user id (metadata.user_id or user).
	User string
}

// Role of a message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn of the conversation.
type Message struct {
	Role  string
	Parts []Part
}

// Part kinds.
const (
	PartText       = "text"
	PartImage      = "image"
	PartFile       = "file"
	PartToolCall   = "tool_call"
	PartToolResult = "tool_result"
	PartThinking   = "thinking"
)

// Part is one piece of a message.
type Part struct {
	Kind string
	Text string

	// Image and file: inline base64 data with its media type, or a URL.
	MediaType string
	Data      string
	URL       string
	Filename  string

	// Tool call: CallID ties the call to its result. Args is the JSON text
	// of the arguments. Custom marks a Responses freeform (custom) tool,
	// whose input is plain text carried as {"input": "..."}.
	CallID string
	Name   string
	Args   string
	Custom bool

	// Tool result: what the tool gave back (text and images).
	Result  []Part
	IsError bool

	// Thinking: Signature is an Anthropic thinking signature; Redacted is
	// an Anthropic redacted_thinking block's data. Sealed is a reasoning
	// item's encrypted_content from another API, passed back untouched
	// only to the API that made it.
	Signature string
	Redacted  string
	Sealed    string

	// Cache marks an Anthropic cache_control breakpoint on this part.
	Cache *CacheMark
}

// CacheMark is an Anthropic cache_control breakpoint.
type CacheMark struct {
	TTL string // "" (5m) or "1h"
}

// Tool is a function the model may call.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	// Custom is a Responses freeform tool: the model writes plain text,
	// carried to other APIs as a function taking {"input": string}.
	Custom bool
}

// Tool choice modes.
const (
	ChoiceAuto = ""
	ChoiceNone = "none"
	ChoiceAny  = "any"
	ChoiceTool = "tool"
)

// ToolChoice says whether and which tool the model must call.
type ToolChoice struct {
	Mode string
	Name string
}

// Event kinds a decoder emits from an upstream's reply.
const (
	EvText      = "text"       // Text: a piece of the answer
	EvThinking  = "thinking"   // Text: a piece of the model's thinking
	EvSignature = "signature"  // Text: the thinking block's signature
	EvRedacted  = "redacted"   // Text: a redacted_thinking block's data
	EvSealed    = "sealed"     // Text: a reasoning item's encrypted_content
	EvToolStart = "tool_start" // CallID, Name: a tool call begins
	EvToolArgs  = "tool_args"  // Text: a piece of its arguments' JSON
	EvStop      = "stop"       // Stop: why the reply ended
	EvUsage     = "usage"      // Usage: token counts (may come more than once)
	EvError     = "error"      // Err: the upstream failed mid-reply
)

// Stop reasons.
const (
	StopEnd     = "end"
	StopLength  = "length"
	StopTool    = "tool"
	StopRefusal = "refusal"
)

// Event is one step of a reply.
type Event struct {
	Kind string
	// Index separates parallel tool calls: arguments go to the call that
	// started with the same Index.
	Index  int
	Text   string
	CallID string
	Name   string
	Stop   string
	Usage  Usage
	Err    *Error
}

// Usage counts tokens. Input excludes cached tokens: Input + CacheRead +
// CacheWrite is everything the model read.
type Usage struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Reasoning  int
}

// add merges a later usage report: a field it reports replaces the earlier
// one (streams repeat running totals, not increments).
func (u *Usage) add(v Usage) {
	if v.Input > 0 {
		u.Input = v.Input
	}
	if v.Output > 0 {
		u.Output = v.Output
	}
	if v.CacheRead > 0 {
		u.CacheRead = v.CacheRead
	}
	if v.CacheWrite > 0 {
		u.CacheWrite = v.CacheWrite
	}
	if v.Reasoning > 0 {
		u.Reasoning = v.Reasoning
	}
}

// Error is an upstream failure in no API's shape.
type Error struct {
	Status  int
	Kind    string // ErrKind*
	Message string
}

// Error kinds, from which each encoder picks its API's error type.
const (
	ErrInvalid  = "invalid"
	ErrAuth     = "auth"
	ErrDenied   = "denied"
	ErrNotFound = "not_found"
	ErrTooLarge = "too_large"
	ErrRate     = "rate"
	ErrOverload = "overloaded"
	ErrServer   = "server"
	// ErrContext is a prompt longer than the model's context window. Each
	// agent compacts its conversation only when told in its own words.
	ErrContext = "context"
)

func (e *Error) Error() string { return e.Message }
