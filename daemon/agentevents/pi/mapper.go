package pi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
)

// Message is one JSON line from the extension.
type Message struct {
	T       string `json:"t"`
	Reason  string `json:"reason"`
	Session string `json:"session"`
	ID      string `json:"id"`
	Running bool   `json:"running"`
	Turn    string `json:"turn"`
	Source  string `json:"source"`
	SHA     string `json:"sha"`
	Aborted bool   `json:"aborted"`
	Stop    string `json:"stop"`
	Error   string `json:"error"`
}

// ParseMessage decodes one line.
func ParseMessage(line []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return Message{}, fmt.Errorf("pi extension message: %w", err)
	}
	if strings.TrimSpace(m.T) == "" {
		return Message{}, fmt.Errorf("pi extension message has no type")
	}
	return m, nil
}

// Mapper turns one Worker's extension messages into events.
type Mapper struct {
	workerID string
	inputs   int
}

// NewMapper returns a Mapper for one Worker.
func NewMapper(workerID string) *Mapper { return &Mapper{workerID: workerID} }

// Map turns one message into the events it carries. "hello" is the daemon's
// to resolve (it names the Worker) and yields none here.
func (m *Mapper) Map(msg Message, at time.Time) []agentevents.Event {
	ev := func(kind agentevents.EventKind) agentevents.Event {
		return agentevents.Event{WorkerID: m.workerID, Kind: kind, At: at, TurnID: msg.Turn}
	}
	switch msg.T {
	case "input":
		if msg.SHA == "" {
			return nil
		}
		m.inputs++
		e := ev(agentevents.KindUserMessage)
		e.TurnID = ""
		e.ID = fmt.Sprintf("pi-input-%d-%d", at.UnixNano(), m.inputs)
		e.InputSHA256, e.InputUnwrappedSHA256 = msg.SHA, msg.SHA
		return []agentevents.Event{e}
	case "turn_start":
		e := ev(agentevents.KindTurnStarted)
		e.Status = agentevents.TurnRunning
		return []agentevents.Event{e}
	case "progress":
		return []agentevents.Event{ev(agentevents.KindProgress)}
	case "turn_end":
		e := ev(agentevents.KindTurnFinished)
		switch {
		case msg.Aborted:
			e.Status = agentevents.TurnInterrupted
		case msg.Stop == "error":
			e.Status = agentevents.TurnFailed
			// Pi reports a failure as text only, with no code or status, so it
			// is not classified: the message is kept to show.
			e.Error = &agentevents.TurnError{Class: agentevents.ErrorUnknown, Message: strings.TrimSpace(msg.Error)}
		default:
			e.Status = agentevents.TurnCompleted
		}
		return []agentevents.Event{e}
	case "bye":
		return []agentevents.Event{ev(agentevents.KindExit)}
	}
	return nil
}

// Binding is the conversation a hello names: the session id, with the session
// file as its reference. By is "launch" for the file Mewla started Pi with and
// "agent" for one Pi moved to (/new, /resume, /fork).
func Binding(msg Message, launchedWith string) agentevents.Binding {
	by := "agent"
	if msg.Session != "" && msg.Session == launchedWith {
		by = "launch"
	}
	return agentevents.Binding{ConversationID: msg.ID, Ref: msg.Session, By: by}
}
