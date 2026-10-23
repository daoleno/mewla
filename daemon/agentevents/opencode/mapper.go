package opencode

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// Frame is one event of the service's stream.
type Frame struct {
	ID      string          `json:"id"`
	Created int64           `json:"created"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
}

// ParseFrame decodes the JSON of one stream event.
func ParseFrame(raw []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return Frame{}, fmt.Errorf("opencode event: %w", err)
	}
	if f.Type == "" {
		return Frame{}, fmt.Errorf("opencode event has no type")
	}
	return f, nil
}

// SessionOf is the session a frame belongs to, empty for a frame about the
// service itself.
func SessionOf(f Frame) string {
	var d struct {
		SessionID string `json:"sessionID"`
	}
	_ = json.Unmarshal(f.Data, &d)
	return d.SessionID
}

// Mapper is the state one Worker's frames need: which execution is running.
// It is used from one goroutine.
type Mapper struct {
	workerID string
	session  string
	turn     string
	asked    map[string]bool
}

// NewMapper returns a Mapper for the Worker following session.
func NewMapper(workerID, session string) *Mapper {
	return &Mapper{workerID: workerID, session: session, asked: map[string]bool{}}
}

// Session is the session the Worker follows.
func (m *Mapper) Session() string { return m.session }

// Running reports that the Mapper has seen an execution start and not end.
func (m *Mapper) Running() bool { return m.turn != "" }

// Bound is the event that names the Worker's conversation: the session id
// Mewla launched it with.
func (m *Mapper) Bound(at time.Time) agentevents.Event {
	return agentevents.Event{
		WorkerID: m.workerID, Kind: agentevents.KindBound, At: at,
		Binding: &agentevents.Binding{ConversationID: m.session, By: "launch"},
	}
}

// Pending reports whether the approval is one the service is still waiting on.
func (m *Mapper) Pending(id string) bool { return m.asked[id] }

// Map turns one frame of the Worker's session into the events it carries.
// A frame of another session yields none.
func (m *Mapper) Map(f Frame, at time.Time) []agentevents.Event {
	if SessionOf(f) != m.session {
		return nil
	}
	ev := func(kind agentevents.EventKind) agentevents.Event {
		return agentevents.Event{WorkerID: m.workerID, Kind: kind, At: at, TurnID: m.turn}
	}
	switch f.Type {
	case "session.inbox.enqueued":
		var d struct {
			InboxID string `json:"inboxID"`
			Item    struct {
				Type    string `json:"type"`
				Payload struct {
					Text string `json:"text"`
				} `json:"payload"`
			} `json:"item"`
		}
		if json.Unmarshal(f.Data, &d) != nil || d.Item.Type != "user" || d.InboxID == "" {
			return nil
		}
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(d.Item.Payload.Text)))
		msg := ev(agentevents.KindUserMessage)
		msg.ID, msg.ClientID = d.InboxID, d.InboxID
		msg.InputSHA256, msg.InputUnwrappedSHA256 = digest, digest
		return []agentevents.Event{msg}
	case "session.execution.started":
		m.turn = f.ID
		started := ev(agentevents.KindTurnStarted)
		started.Status = agentevents.TurnRunning
		return []agentevents.Event{started}
	case "session.execution.succeeded":
		return m.finish(at, agentevents.TurnCompleted, nil)
	case "session.execution.interrupted":
		return m.finish(at, agentevents.TurnInterrupted, nil)
	case "session.execution.failed":
		var d struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(f.Data, &d)
		return m.finish(at, agentevents.TurnFailed, ErrorFrom(d.Error.Type, d.Error.Message))
	case "permission.asked":
		var d struct {
			ID        string   `json:"id"`
			Action    string   `json:"action"`
			Resources []string `json:"resources"`
		}
		if json.Unmarshal(f.Data, &d) != nil || d.ID == "" {
			return nil
		}
		m.asked[d.ID] = true
		approval := Approval(d.ID, m.session, d.Action, d.Resources)
		request := ev(agentevents.KindApprovalRequest)
		request.Approval = &approval
		return []agentevents.Event{request}
	case "permission.replied":
		var d struct {
			RequestID string `json:"requestID"`
			Reply     string `json:"reply"`
		}
		if json.Unmarshal(f.Data, &d) != nil || !m.asked[d.RequestID] {
			return nil
		}
		delete(m.asked, d.RequestID)
		resolved := ev(agentevents.KindApprovalResolved)
		resolved.ApprovalID, resolved.By, resolved.OptionID = d.RequestID, "agent", d.Reply
		return []agentevents.Event{resolved}
	}
	if m.turn != "" && isProgress(f.Type) {
		return []agentevents.Event{ev(agentevents.KindProgress)}
	}
	return nil
}

// Asked is the approval request of a permission found waiting on the service
// (listed, not streamed), for a Worker whose request was made before Mewla
// was listening. It yields nothing for one already known.
func (m *Mapper) Asked(id, action string, resources []string, at time.Time) []agentevents.Event {
	if id == "" || m.asked[id] {
		return nil
	}
	m.asked[id] = true
	approval := Approval(id, m.session, action, resources)
	return []agentevents.Event{{WorkerID: m.workerID, Kind: agentevents.KindApprovalRequest, At: at, TurnID: m.turn, Approval: &approval}}
}

// Settled marks a pending approval as answered by someone other than the
// stream's own replied event (Mewla answered it).
func (m *Mapper) Settled(id string) { delete(m.asked, id) }

func (m *Mapper) finish(at time.Time, status agentevents.TurnStatus, failure *agentevents.TurnError) []agentevents.Event {
	if m.turn == "" {
		// An end whose start was missed (the stream reconnected mid-turn)
		// still ends the turn the Hub holds as running.
		m.turn = "unseen"
	}
	done := agentevents.Event{WorkerID: m.workerID, Kind: agentevents.KindTurnFinished, At: at, TurnID: m.turn, Status: status, Error: failure}
	m.turn = ""
	return []agentevents.Event{done}
}

// isProgress names the frames that show the agent working within an execution.
// Text deltas are left out: the steps and tools around them show the same
// liveness at a fraction of the volume.
func isProgress(kind string) bool {
	switch kind {
	case "session.step.started", "session.step.ended", "session.tool.called", "session.tool.success",
		"session.tool.error", "session.text.ended", "session.usage.updated", "permission.asked":
		return true
	}
	return false
}

// ErrorFrom maps the service's structured error type onto the closed set of
// error classes. A type it does not know is "unknown"; the message is kept for
// display and never classified.
func ErrorFrom(errType, message string) *agentevents.TurnError {
	class := agentevents.ErrorUnknown
	switch strings.TrimSpace(errType) {
	case "provider.auth":
		class = agentevents.ErrorAuth
	case "provider.quota":
		class = agentevents.ErrorBilling
	case "provider.rate-limit":
		class = agentevents.ErrorRateLimit
	case "provider.timeout", "provider.transport", "provider.connect":
		class = agentevents.ErrorNetwork
	case "provider.internal":
		class = agentevents.ErrorServer
	case "provider.invalid-request", "provider.invalid-output", "provider.no-route",
		"provider.unsupported-operation", "provider.content-filter":
		class = agentevents.ErrorInvalidRequest
	}
	return &agentevents.TurnError{Class: class, Code: strings.TrimSpace(errType), Message: strings.TrimSpace(message)}
}

// Approval builds the approval request of a permission the service asked for.
// The options are the three answers the service takes.
func Approval(id, session, action string, resources []string) agentevents.Approval {
	kind := "permission"
	switch action {
	case "shell":
		kind = "command"
	case "edit":
		kind = "file_change"
	}
	summary := strings.Join(resources, "\n")
	if summary == "" {
		summary = action
	}
	return agentevents.Approval{
		ID: id, ConversationID: session, Kind: kind, Tool: action, Summary: summary,
		Options: []agentevents.ApprovalOption{
			{ID: "once", Label: "Allow once", Scope: "once"},
			{ID: "always", Label: "Always allow", Scope: "always"},
			{ID: "reject", Label: "Reject", Scope: "deny"},
		},
	}
}

// Decision is the service's reply for a chosen option; ok is false for an
// option the service does not take.
func Decision(optionID string) (reply string, ok bool) {
	switch optionID {
	case "once", "always", "reject":
		return optionID, true
	}
	return "", false
}
