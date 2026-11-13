package server

import (
	"encoding/json"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

type fakeApprovals map[string][]ApprovalWire

func (f fakeApprovals) Pending(workerID string) []ApprovalWire    { return f[workerID] }
func (f fakeApprovals) Decide(string, string, string) error       { return nil }
func (f fakeApprovals) AnsweredInTerminal(string, string, string) {}

func TestWorkerWireCarriesTheDecisionsItsAgentIsWaitingOn(t *testing.T) {
	s := &Server{}
	s.SetAgentApprovals(fakeApprovals{"%1": {{
		ID: "a1", Kind: "command", Tool: "shell", Summary: "touch x",
		Options: []ApprovalOptionWire{{ID: "accept", Label: "Allow", Scope: "once"}, {ID: "cancel", Label: "Deny", Scope: "deny"}},
	}}})
	waiting, _ := json.Marshal(s.workerSessionWire(&classifier.Worker{ID: "%1"}))
	var got struct {
		Approvals []ApprovalWire `json:"approvals"`
	}
	if err := json.Unmarshal(waiting, &got); err != nil || len(got.Approvals) != 1 || got.Approvals[0].Options[1].ID != "cancel" {
		t.Fatalf("waiting Worker wire = %s (%v)", waiting, err)
	}
	idle, _ := json.Marshal(s.workerSessionWire(&classifier.Worker{ID: "%2"}))
	var none map[string]json.RawMessage
	_ = json.Unmarshal(idle, &none)
	if _, present := none["approvals"]; present {
		t.Fatalf("a Worker waiting on nothing carries approvals: %s", idle)
	}
}

// An agent's decision appearing or clearing reaches the app as the feed's next change: the app applies only
// sequenced changes, so an unsequenced update left the Needs you row (and its Approve) to the Session's next
// publish. The hidden Brain host stays off the list.
func TestApprovalsChangeIsTheFeedsNextSequencedChange(t *testing.T) {
	srv := New(nil, nil, nil, nil, nil, nil, nil)
	live := map[string]*classifier.Worker{
		"%1":   {ID: "%1", Name: "claude", State: classifier.StateRunning},
		"host": {ID: "host", Name: "brain", State: classifier.StateRunning, Hidden: true},
	}
	srv.getWorkerOverride = func(id string) *classifier.Worker { return live[id] }
	approvals := fakeApprovals{"%1": {{ID: "a1", Kind: "command", Summary: "git push", Options: []ApprovalOptionWire{{ID: "yes", Label: "Allow", Scope: "once"}}}}}
	srv.SetAgentApprovals(approvals)
	conn := feedTestSocket(t, srv)
	defer srv.shutdownAuthenticatedClients()
	if list, _ := readFeedType(t, conn, "worker_session_list"); list.Seq != 0 {
		t.Fatalf("connect list seq = %d", list.Seq)
	}

	srv.WorkerApprovalsChanged("host")
	srv.WorkerApprovalsChanged("%1")
	asked, raw := readFeedType(t, conn, "worker_session_created", "worker_session_updated")
	var wire struct {
		WorkerSession struct {
			Approvals []ApprovalWire `json:"approvals"`
		} `json:"worker_session"`
	}
	_ = json.Unmarshal(raw, &wire)
	if asked.Seq != 1 || asked.WorkerSession == nil || asked.WorkerSession.ID != "%1" || len(wire.WorkerSession.Approvals) != 1 {
		t.Fatalf("first feed change after an approval = %s, want %%1 with its approval at seq 1", raw)
	}

	delete(approvals, "%1")
	srv.WorkerApprovalsChanged("%1")
	cleared, raw := readFeedType(t, conn, "worker_session_updated")
	wire.WorkerSession.Approvals = nil
	_ = json.Unmarshal(raw, &wire)
	if cleared.Seq != 2 || len(wire.WorkerSession.Approvals) != 0 {
		t.Fatalf("change after the approval cleared = %s, want seq 2 without approvals", raw)
	}
}
