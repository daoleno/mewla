// Package agentevents is the one way the daemon learns what a coding agent is
// doing. An adapter for each agent (hooks, an app-server connection, a watched
// file) turns that agent's own signals into the normalized Events below and
// publishes them to a Hub. Core code reads only the Hub, so it never has to
// know which agent produced a fact or guess a conversation from the screen.
package agentevents

import "time"

// Level says how an adapter performs one capability. Core code never branches
// on an agent's name; it branches on the level the adapter declared.
type Level string

const (
	// LevelNone: the agent offers no way, so the capability is unavailable.
	LevelNone Level = "none"
	// LevelKeystroke: the capability works by typing into the agent's
	// terminal. It is the lowest fidelity and exists only in adapters that
	// declare it.
	LevelKeystroke Level = "keystroke"
	// LevelFile: Mewla learns it by watching the agent's one known file.
	LevelFile Level = "file"
	// LevelHook: the agent calls out to Mewla (hooks, an extension).
	LevelHook Level = "hook"
	// LevelStructured: the agent has a typed interface Mewla calls or
	// subscribes to (an app-server, an HTTP API).
	LevelStructured Level = "structured"
)

// Capability is one thing an adapter can do for a Worker.
type Capability string

const (
	CapBinding   Capability = "binding"   // knows the Worker's conversation id from birth
	CapEvents    Capability = "events"    // pushes turn and message events
	CapSend      Capability = "send"      // submits a user turn and returns a receipt
	CapApprove   Capability = "approve"   // receives and answers approval requests
	CapInterrupt Capability = "interrupt" // stops the running turn
)

// Descriptor is what an adapter declares about itself.
type Descriptor struct {
	// Provider names the agent for display and grouping. It never selects
	// behavior.
	Provider     string
	Capabilities map[Capability]Level
}

// Level is the declared level of c, LevelNone when undeclared.
func (d Descriptor) Level(c Capability) Level {
	if level, ok := d.Capabilities[c]; ok {
		return level
	}
	return LevelNone
}

// EventKind is the type of a normalized Event.
type EventKind string

const (
	// KindBound: the Worker's conversation identity became known or changed.
	KindBound EventKind = "bound"
	// KindTurnStarted: the agent began a turn.
	KindTurnStarted EventKind = "turn_started"
	// KindTurnFinished: the agent ended a turn (completed, failed or
	// interrupted).
	KindTurnFinished EventKind = "turn_finished"
	// KindUserMessage: the agent accepted a user message into the
	// conversation. It is the proof that an input was delivered.
	KindUserMessage EventKind = "user_message"
	// KindProgress: the agent did something within the current turn.
	KindProgress EventKind = "progress"
	// KindApprovalRequest: the agent is waiting on a decision.
	KindApprovalRequest EventKind = "approval_request"
	// KindApprovalResolved: a pending decision was made (by Mewla, by the
	// agent's own UI, or by timeout).
	KindApprovalResolved EventKind = "approval_resolved"
	// KindExit: the agent's session ended.
	KindExit EventKind = "exit"
)

// TurnStatus is how a turn stands.
type TurnStatus string

const (
	TurnRunning     TurnStatus = "running"
	TurnCompleted   TurnStatus = "completed"
	TurnFailed      TurnStatus = "failed"
	TurnInterrupted TurnStatus = "interrupted"
)

// Terminal reports whether the turn can no longer change.
func (s TurnStatus) Terminal() bool {
	return s == TurnCompleted || s == TurnFailed || s == TurnInterrupted
}

// ErrorClass is the closed set an adapter maps an agent's structured error
// onto. It never comes from message text.
type ErrorClass string

const (
	ErrorAuth           ErrorClass = "auth"
	ErrorBilling        ErrorClass = "billing"
	ErrorRateLimit      ErrorClass = "rate_limit"
	ErrorOverloaded     ErrorClass = "overloaded"
	ErrorInvalidRequest ErrorClass = "invalid_request"
	ErrorContext        ErrorClass = "context"
	ErrorNetwork        ErrorClass = "network"
	ErrorServer         ErrorClass = "server"
	ErrorSandbox        ErrorClass = "sandbox"
	ErrorUnknown        ErrorClass = "unknown"
)

// TurnError is a turn's structured failure.
type TurnError struct {
	Class      ErrorClass
	HTTPStatus int
	// Code is the agent's own error code or variant name, kept for display.
	Code    string
	Message string
	// WillRetry is true while the agent is still retrying the same turn, so
	// the failure is not final.
	WillRetry bool
}

// Binding is the Worker's conversation identity.
type Binding struct {
	// ConversationID is the agent's session or thread id.
	ConversationID string
	// Ref locates the transcript when the agent has a file for it (a path);
	// empty when the conversation is only reachable through the agent.
	Ref string
	// By records how the identity was learned: "launch" (Mewla chose it),
	// "agent" (the agent announced it) or "file" (found by watching).
	By string
}

// Approval is a pending decision the agent is waiting on.
type Approval struct {
	ID string
	// ConversationID is the conversation that asked. An answer resolves the
	// request only while the Worker is still in that conversation.
	ConversationID string
	Kind           string // command, file_change, permission, question, auto_denied
	Tool           string
	Summary        string
	// Reason says why the agent is asking: for auto_denied, the rule the Auto
	// mode classifier named when it refused the action.
	Reason  string
	Options []ApprovalOption
}

// ApprovalOption is one answer the agent offers, with the agent's own id.
type ApprovalOption struct {
	ID    string
	Label string
	// Scope: once, session, always or deny.
	Scope string
}

// Event is one normalized fact about one Worker.
type Event struct {
	WorkerID string
	Kind     EventKind
	At       time.Time

	// TurnID is the agent's identity for the turn the event belongs to.
	TurnID string
	Status TurnStatus
	Error  *TurnError

	// ID identifies a user message; InputSHA256 and InputUnwrappedSHA256 are
	// the digests of its exact text, the same way the transcript readers
	// compute them, so the watcher can match it to the input it sent.
	ID                   string
	InputSHA256          string
	InputUnwrappedSHA256 string
	// ClientID echoes the id Mewla gave the agent when it sent the input.
	ClientID string

	Binding  *Binding
	Approval *Approval
	// ApprovalID, By and OptionID describe an approval_resolved event.
	ApprovalID string
	By         string
	OptionID   string

	// ExitCode is set on KindExit when the agent reports one.
	ExitCode *int
}
