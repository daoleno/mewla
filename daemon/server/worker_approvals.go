package server

import (
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/gorilla/websocket"
)

// ApprovalWire is one decision an agent is waiting on, as the app sees it. The
// app shows these instead of reading a permission prompt off the screen.
type ApprovalWire struct {
	ID      string               `json:"id"`
	Kind    string               `json:"kind,omitempty"`
	Tool    string               `json:"tool,omitempty"`
	Summary string               `json:"summary,omitempty"`
	Options []ApprovalOptionWire `json:"options"`
}

// ApprovalOptionWire is one answer the agent offers.
type ApprovalOptionWire struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Scope string `json:"scope,omitempty"`
}

// ApprovalDecisionError refuses an answer with a code the app can act on.
type ApprovalDecisionError struct {
	Code    string
	Message string
}

func (e *ApprovalDecisionError) Error() string { return e.Message }

// AgentApprovals is the daemon's view of what agents are waiting on.
type AgentApprovals interface {
	// Pending lists the decisions one Worker is waiting on.
	Pending(workerID string) []ApprovalWire
	// Decide answers one with an option the agent offered.
	Decide(workerID, approvalID, optionID string) error
}

// SetAgentApprovals installs where approvals come from and go to.
func (s *Server) SetAgentApprovals(approvals AgentApprovals) {
	s.mu.Lock()
	s.agentApprovals = approvals
	s.mu.Unlock()
}

func (s *Server) pendingApprovals(workerID string) []ApprovalWire {
	s.mu.Lock()
	approvals := s.agentApprovals
	s.mu.Unlock()
	if approvals == nil {
		return nil
	}
	return approvals.Pending(workerID)
}

// WorkerApprovalsChanged tells every connected app that a Worker's pending
// decisions changed.
func (s *Server) WorkerApprovalsChanged(workerID string) {
	worker := s.lookupWorker(workerID)
	if worker == nil {
		return
	}
	s.broadcastJSON(map[string]any{"type": "worker_session_updated", "worker_session": s.workerSessionWire(worker)})
}

func (s *Server) handleApprovalDecision(conn *websocket.Conn, raw clientMessage) {
	s.mu.Lock()
	approvals := s.agentApprovals
	s.mu.Unlock()
	workerID := strings.TrimSpace(raw.WorkerID)
	if approvals == nil || workerID == "" || strings.TrimSpace(raw.ID) == "" || strings.TrimSpace(raw.OptionID) == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "approval_invalid", "An approval answer needs a worker, an approval and an option.")
		return
	}
	if err := approvals.Decide(workerID, strings.TrimSpace(raw.ID), strings.TrimSpace(raw.OptionID)); err != nil {
		code, message := "approval_failed", err.Error()
		if refused, ok := err.(*ApprovalDecisionError); ok {
			code, message = refused.Code, refused.Message
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, code, message)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":        "approval_decided",
		"request_id":  raw.RequestID,
		"worker_id":   workerID,
		"approval_id": raw.ID,
	})
}

func approvalsOf(worker *classifier.Worker, s *Server) []ApprovalWire {
	if worker == nil {
		return nil
	}
	return s.pendingApprovals(worker.ID)
}
