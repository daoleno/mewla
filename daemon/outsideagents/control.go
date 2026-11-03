package outsideagents

import (
	"errors"
	"fmt"
	"strings"
)

// ControlRequest is what `mewla agents mcp` sends the running daemon.
type ControlRequest struct {
	// Action is connect, status, pending, agents, approve, deny, revoke,
	// requests, reply.
	Action    string `json:"action"`
	Name      string `json:"name,omitempty"`
	TokenHash string `json:"token_hash,omitempty"`
	ID        string `json:"id,omitempty"`
	Number    string `json:"number,omitempty"`
	Text      string `json:"text,omitempty"`
	WorkID    string `json:"work_id,omitempty"`
}

// ControlResult answers a ControlRequest.
type ControlResult struct {
	Pending  *Pending  `json:"pending,omitempty"`
	Pendings []Pending `json:"pendings,omitempty"`
	Agents   []Agent   `json:"agents,omitempty"`
	Request  *Request  `json:"request,omitempty"`
	Requests []Request `json:"requests,omitempty"`
}

// Handle runs a control request. Approval from the terminal needs the number
// the agent's own terminal showed, so a script that only knows the id cannot
// approve itself by accident: it must read the number.
func (m *Manager) Handle(req ControlRequest) (ControlResult, error) {
	switch req.Action {
	case "connect":
		pending, err := m.Connect(req.Name, req.TokenHash)
		if err != nil {
			return ControlResult{}, err
		}
		return ControlResult{Pending: &pending}, nil
	case "status":
		pending, err := m.Status(req.ID)
		if err != nil {
			return ControlResult{}, err
		}
		return ControlResult{Pending: &pending}, nil
	case "pending":
		return ControlResult{Pendings: m.PendingList()}, nil
	case "agents":
		return ControlResult{Agents: m.Agents()}, nil
	case "approve", "deny":
		if strings.TrimSpace(req.ID) == "" && strings.TrimSpace(req.Number) == "" {
			return ControlResult{}, errors.New("give the number the agent shows, or its request id")
		}
		pending, err := m.Decide(strings.TrimSpace(req.ID), strings.TrimSpace(req.Number), req.Action == "approve", "this computer's terminal")
		if err != nil {
			return ControlResult{}, err
		}
		return ControlResult{Pending: &pending}, nil
	case "revoke":
		if err := m.Revoke(strings.TrimSpace(req.ID)); err != nil {
			return ControlResult{}, err
		}
		return ControlResult{}, nil
	case "requests":
		return ControlResult{Requests: m.Requests("", 50)}, nil
	case "reply":
		request, err := m.Reply(strings.TrimSpace(req.ID), req.Text, req.WorkID)
		if err != nil {
			return ControlResult{}, err
		}
		return ControlResult{Request: &request}, nil
	}
	return ControlResult{}, fmt.Errorf("unknown agents action %q", req.Action)
}
