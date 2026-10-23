package server

import (
	"encoding/json"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

type fakeApprovals map[string][]ApprovalWire

func (f fakeApprovals) Pending(workerID string) []ApprovalWire { return f[workerID] }
func (f fakeApprovals) Decide(string, string, string) error    { return nil }

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
