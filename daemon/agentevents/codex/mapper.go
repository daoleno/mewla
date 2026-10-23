// Package codex adapts Codex's app-server protocol to the normalized agent
// events.
//
// Mewla runs each Codex Worker's TUI against a headless app-server of its own
// (codex --remote) and watches that server as a second client. The server
// pushes the thread id when the TUI starts, every turn and item as it
// happens, and each approval as a request a client may answer; Mapper turns
// those messages into events, with no file read.
package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// Provider is the executor provider this adapter serves.
const Provider = "codex"

// Mapper is the state one Worker's message stream needs: which thread is the
// Worker's, and which turn is running. It is used from one goroutine.
type Mapper struct {
	workerID string
	thread   string
	// activeSince is when the thread went active with no turn id yet: the
	// first turn's turn/started precedes the observer's attach and is never
	// delivered, so the turn is named by the first message that carries its id.
	activeSince time.Time
	turn        string
	// decisions remembers, per pending approval, the exact value each offered
	// option stands for.
	decisions map[string]map[string]any
}

// NewMapper returns a Mapper for one Worker.
func NewMapper(workerID string) *Mapper {
	return &Mapper{workerID: workerID, decisions: map[string]map[string]any{}}
}

// Thread is the Worker's thread id, empty until the server announced it.
func (m *Mapper) Thread() string { return m.thread }

// Turn is the id of the running turn, empty when none is known.
func (m *Mapper) Turn() string { return m.turn }

// Active reports that the Worker's thread is running a turn the Mapper has
// not yet seen the id of, which is when the observer must attach.
func (m *Mapper) Active() bool { return !m.activeSince.IsZero() && m.turn == "" }

type threadEnvelope struct {
	Thread struct {
		ID             string  `json:"id"`
		Path           string  `json:"path"`
		Ephemeral      bool    `json:"ephemeral"`
		ParentThreadID *string `json:"parentThreadId"`
	} `json:"thread"`
}

// Notification maps one server notification.
func (m *Mapper) Notification(method string, params json.RawMessage, at time.Time) []agentevents.Event {
	ev := func(kind agentevents.EventKind) agentevents.Event {
		return agentevents.Event{WorkerID: m.workerID, Kind: kind, At: at}
	}
	switch method {
	case "thread/started":
		var p threadEnvelope
		if json.Unmarshal(params, &p) != nil || p.Thread.ID == "" {
			return nil
		}
		// A helper thread (a generated title) and a side conversation are not
		// the Worker's thread, whichever the server loaded first.
		if p.Thread.Ephemeral || p.Thread.ParentThreadID != nil {
			return nil
		}
		if p.Thread.ID == m.thread {
			return nil
		}
		m.thread, m.turn, m.activeSince = p.Thread.ID, "", time.Time{}
		bound := ev(agentevents.KindBound)
		bound.Binding = &agentevents.Binding{ConversationID: p.Thread.ID, Ref: p.Thread.Path, By: "agent"}
		return []agentevents.Event{bound}
	case "thread/status/changed":
		var p struct {
			ThreadID string `json:"threadId"`
			Status   struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread || m.thread == "" {
			return nil
		}
		switch {
		case p.Status.Type == "active" && m.turn == "" && m.activeSince.IsZero():
			m.activeSince = at
		case p.Status.Type == "idle" && m.turn == "":
			// A turn that ended before the observer attached left no id to
			// name it by; the fallback reads it from the transcript.
			m.activeSince = time.Time{}
		}
		return nil
	case "turn/started":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread || p.Turn.ID == "" {
			return nil
		}
		return m.begin(p.Turn.ID, at)
	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string          `json:"id"`
				Status string          `json:"status"`
				Error  json.RawMessage `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread || p.Turn.ID == "" {
			return nil
		}
		events := m.begin(p.Turn.ID, at)
		done := ev(agentevents.KindTurnFinished)
		done.TurnID = p.Turn.ID
		switch p.Turn.Status {
		case "interrupted":
			done.Status = agentevents.TurnInterrupted
		case "failed":
			done.Status = agentevents.TurnFailed
			done.Error = ErrorFrom(p.Turn.Error, false)
		default:
			done.Status = agentevents.TurnCompleted
		}
		m.turn, m.activeSince = "", time.Time{}
		return append(events, done)
	case "error":
		var p struct {
			ThreadID  string          `json:"threadId"`
			TurnID    string          `json:"turnId"`
			WillRetry bool            `json:"willRetry"`
			Error     json.RawMessage `json:"error"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread || !p.WillRetry {
			// A final failure arrives again on turn/completed.
			return nil
		}
		events := m.begin(p.TurnID, at)
		retry := ev(agentevents.KindTurnFinished)
		retry.TurnID, retry.Status = p.TurnID, agentevents.TurnFailed
		retry.Error = ErrorFrom(p.Error, true)
		return append(events, retry)
	case "serverRequest/resolved":
		var p struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread {
			return nil
		}
		id := RequestKey(p.RequestID)
		delete(m.decisions, id)
		resolved := ev(agentevents.KindApprovalResolved)
		resolved.ApprovalID, resolved.By = id, "agent"
		return []agentevents.Event{resolved}
	}
	if strings.HasPrefix(method, "item/") || method == "thread/tokenUsage/updated" || method == "turn/diff/updated" || method == "turn/plan/updated" {
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Item     struct {
				Type     string `json:"type"`
				ID       string `json:"id"`
				ClientID string `json:"clientId"`
				Content  []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil || p.ThreadID != m.thread || p.TurnID == "" {
			return nil
		}
		events := m.begin(p.TurnID, at)
		if method == "item/started" && p.Item.Type == "userMessage" {
			var text strings.Builder
			for _, part := range p.Item.Content {
				if part.Type == "text" {
					text.WriteString(part.Text)
				}
			}
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(text.String())))
			msg := ev(agentevents.KindUserMessage)
			msg.TurnID, msg.ID, msg.ClientID = p.TurnID, p.TurnID, p.Item.ClientID
			msg.InputSHA256, msg.InputUnwrappedSHA256 = digest, digest
			events = append(events, msg)
		}
		progress := ev(agentevents.KindProgress)
		progress.TurnID = p.TurnID
		return append(events, progress)
	}
	return nil
}

// begin starts the turn id names, once, at the time the thread went active if
// that was earlier than now.
func (m *Mapper) begin(turnID string, at time.Time) []agentevents.Event {
	if turnID == "" || turnID == m.turn {
		return nil
	}
	started := agentevents.Event{WorkerID: m.workerID, Kind: agentevents.KindTurnStarted, At: at, TurnID: turnID, Status: agentevents.TurnRunning}
	if !m.activeSince.IsZero() && m.activeSince.Before(at) {
		started.At = m.activeSince
	}
	m.turn, m.activeSince = turnID, time.Time{}
	return []agentevents.Event{started}
}
