package server

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/remote"
	"github.com/gorilla/websocket"
)

// openRequestsTTL bounds how often the count is projected: every computer
// channel asks for it each tick.
const openRequestsTTL = 5 * time.Second

type openRequestsCache struct {
	mu    sync.Mutex
	at    time.Time
	count int
}

// OpenRequestCount is how many requests on this computer wait for the
// person, the count other computers show beside its name ("1 needs you"):
//
//   - each Brain Work that asks a question and is not done, cancelled or
//     snoozed (current_work: question set, snoozed_until absent or past);
//   - each visible Session: its pending approvals for the app, or one when it
//     needs you without one (blocked on an identified prompt, or
//     needs_attention on a Session the owner started: a delegated Worker's
//     own report is Brain's);
//   - Brain's own Session, one while it is blocked on an identified prompt.
//
// A blocked action Brain holds is Brain's, not the person's, until Brain
// hands it over. DSH interactions are not counted (they live in the
// Session's own runtime).
func (s *Server) OpenRequestCount() int {
	s.openRequests.mu.Lock()
	defer s.openRequests.mu.Unlock()
	now := time.Now()
	if !s.openRequests.at.IsZero() && now.Sub(s.openRequests.at) < openRequestsTTL {
		return s.openRequests.count
	}
	s.openRequests.count = s.countOpenRequests(now)
	s.openRequests.at = now
	return s.openRequests.count
}

func (s *Server) countOpenRequests(now time.Time) int {
	count := 0
	if s.brain != nil {
		if snapshot, err := s.liveSnapshot(); err == nil {
			count += openWorkQuestions(snapshot.CurrentWork, now)
		}
		if hostID := s.brain.CurrentHostSessionID(); hostID != "" {
			if host := s.lookupWorker(hostID); host != nil && host.State == classifier.StateBlocked && host.Prompt != nil {
				count++
			}
		}
	}
	for _, worker := range s.currentVisibleWorkerSessions() {
		approvals := len(s.pendingApprovals(worker.ID))
		switch {
		case approvals > 0:
			count += approvals
		case asksOwner(worker, 0):
			count++
		}
	}
	return count
}

func openWorkQuestions(current []brain.CurrentWork, now time.Time) int {
	count := 0
	for _, item := range current {
		if strings.TrimSpace(item.Question) == "" || item.Status == brain.WorkDone || item.Status == brain.WorkCancelled {
			continue
		}
		if item.SnoozedUntil != nil && item.SnoozedUntil.After(now) {
			continue
		}
		count++
	}
	return count
}

// computerWire is one paired computer as the app's computer page reads it.
type computerWire struct {
	DaemonID string     `json:"daemon_id,omitempty"`
	Name     string     `json:"name"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	// OpenRequests is reported by the computer itself over its live channel:
	// absent while offline or from a computer that predates it (unknown,
	// never zero).
	OpenRequests *int   `json:"open_requests,omitempty"`
	Version      string `json:"version,omitempty"`
}

// connectionWire is this computer's remote access connection, from
// remote-status.json. Absent when remote access is off.
type connectionWire struct {
	Phase         string     `json:"phase"`
	Relay         string     `json:"relay,omitempty"`
	Cause         string     `json:"cause,omitempty"`
	ConnectedAt   *time.Time `json:"connected_at,omitempty"`
	LastFailure   string     `json:"last_failure,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
}

func (s *Server) computersMessage(requestID string) map[string]any {
	peers := []computerWire{}
	if s.computers != nil {
		if result, err := s.computers.Handle(context.Background(), computers.Request{Action: "list"}); err == nil {
			for _, view := range result.Computers {
				peers = append(peers, computerWire{
					DaemonID: view.DaemonID, Name: view.Name, Online: view.Online,
					LastSeen: view.LastSeen, OpenRequests: view.OpenRequests, Version: view.Version,
				})
			}
		}
	}
	message := map[string]any{
		"type":          "computers",
		"computers":     peers,
		"open_requests": s.OpenRequestCount(),
	}
	if requestID != "" {
		message["request_id"] = requestID
	}
	if s.auth != nil {
		if status, ok := remote.LoadStatus(s.auth.StorageDir()); ok {
			message["connection"] = connectionWire{
				Phase: status.Phase, Relay: status.Relay, Cause: status.LastError,
				ConnectedAt: status.ConnectedAt, LastFailure: status.LastFailure, LastFailureAt: status.LastFailureAt,
			}
		}
	}
	return message
}

func (s *Server) handleComputersList(conn *websocket.Conn, raw clientMessage) {
	s.sendJSON(conn, s.computersMessage(raw.RequestID))
}

// NotifyComputers tells every app what the computer page shows changed: a
// paired computer came or went or reported a new count, or this computer's
// connection changed.
func (s *Server) NotifyComputers() {
	s.broadcastJSON(s.computersMessage(""))
}
