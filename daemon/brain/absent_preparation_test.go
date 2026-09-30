package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestAbsentPreparationRetirementPreservesFences(t *testing.T) {
	for _, mode := range []string{"gone", "present", "unavailable", "ambiguous", "accepted", "accept-during-probe", "transport-started"} {
		t.Run(mode, func(t *testing.T) {
			s, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			w, err := s.CreateWork(Work{Title: "recover", Objective: "fenced release"})
			if err != nil {
				t.Fatal(err)
			}
			in := delegatedSubmissionCandidate(w.ID, "gone:@1", "turn:gone", "brief", time.Now().UTC())
			in.SignalProtocol = true
			if _, _, err := s.PrepareInputAdmission(in); err != nil {
				t.Fatal(err)
			}
			accept := func() {
				t.Helper()
				_, err := s.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: in.SessionID, TurnID: in.ProposedTurnID, Class: watcher.EvidenceControl, Kind: "running", SourceID: "accepted", At: time.Now().UTC()})
				if err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "ambiguous":
				if err := s.MarkInputAdmissionAmbiguous(in.SessionID, in.ProposedTurnID, "unknown"); err != nil {
					t.Fatal(err)
				}
			case "accepted":
				accept()
			case "transport-started":
				if err := s.MarkInputAdmissionTransportStarted(in.SessionID, in.ProposedTurnID); err != nil {
					t.Fatal(err)
				}
			}
			count, err := s.ReconcileAbsentPreparedAdmissions(w.ID, func(id string) (bool, error) {
				if id != in.SessionID {
					t.Fatal("wrong Session probed")
				}
				if mode == "unavailable" {
					return false, errors.New("inventory unavailable")
				}
				if mode == "accept-during-probe" {
					accept()
				}
				return mode != "present", nil
			})
			want := mode == "gone" || mode == "transport-started"
			if (err != nil) != (mode == "unavailable") || (count == 1) != want {
				t.Fatalf("count=%d err=%v", count, err)
			}
			st, _ := s.FSM().State(lifecycle.WorkID(w.ID))
			if (st.AdmissionByToken(lifecycle.TurnToken(in.ProposedTurnID)).Status == lifecycle.AdmissionRetired) != want {
				t.Fatal("unsafe retirement")
			}
			if want {
				reopened, err := NewStore(s.Root)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := reopened.FSM().AcceptAdmissionBySignal(lifecycle.WorkID(w.ID), lifecycle.TurnToken(in.ProposedTurnID), in.SessionID); err == nil {
					t.Fatal("late signal resurrected retired authority")
				}
				fresh := delegatedSubmissionCandidate(w.ID, "new:@2", "turn:new", "new explicit brief", time.Now().UTC())
				fresh.SignalProtocol = true
				if _, created, err := reopened.PrepareInputAdmission(fresh); err != nil || !created {
					t.Fatalf("new submission stranded: %v", err)
				}
			}
		})
	}
}
