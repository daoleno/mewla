package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

type brainServiceTestWatcher struct {
	sessions          map[string]*classifier.Worker
	turnStore         *brain.Store
	readyInputCalls   int
	receiptInputCalls int
}

func (w *brainServiceTestWatcher) Workers() []*classifier.Worker {
	return nil
}

func (w *brainServiceTestWatcher) GetWorker(id string) *classifier.Worker {
	if w.sessions == nil {
		return nil
	}
	return w.sessions[id]
}

func (w *brainServiceTestWatcher) HasSession(target string) bool {
	presence, err := w.ProbeSession(target)
	return err == nil && presence == watcher.SessionPresencePresent
}

func (w *brainServiceTestWatcher) ProbeSession(target string) (watcher.SessionPresence, error) {
	if w.sessions == nil {
		return watcher.SessionPresenceAbsent, nil
	}
	if _, ok := w.sessions[target]; ok {
		return watcher.SessionPresencePresent, nil
	}
	return watcher.SessionPresenceAbsent, nil
}

func (w *brainServiceTestWatcher) ResolveDelegatedAbsence(target string) (bool, error) {
	presence, err := w.ProbeSession(target)
	if err != nil {
		return false, err
	}
	return presence == watcher.SessionPresenceAbsent, nil
}

func (w *brainServiceTestWatcher) CreateSession(string, watcher.CreateSessionOptions) (string, error) {
	return "", nil
}

func (w *brainServiceTestWatcher) SendInput(string, string) error {
	return nil
}

func (w *brainServiceTestWatcher) SendInputWhenReady(string, string, string) error {
	return nil
}

func (w *brainServiceTestWatcher) SendInputWithReceiptResult(_, _, receipt string) (watcher.InputResult, error) {
	w.receiptInputCalls++
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
}

func (w *brainServiceTestWatcher) SendInputWithReceiptWhenReadyResult(
	_, _, _ string,
	receiptFor watcher.InputReceiptForGeneration,
) (watcher.InputResult, watcher.OwnedGeneration, error) {
	w.readyInputCalls++
	owned := watcher.OwnedGeneration{SessionID: "startup", Generation: "startup-generation"}
	receipt := receiptFor(owned)
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, owned, nil
}
func (w *brainServiceTestWatcher) SubmitBrainHostInput(sessionID, payload, claimToken, workID, providerTurnID string, acceptedAt time.Time) (watcher.InputResult, error) {
	if w.turnStore == nil {
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: providerTurnID, TurnID: providerTurnID}, nil
	}
	existingTurnID := ""
	if current, found, err := w.turnStore.Turn(sessionID); err != nil {
		return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: providerTurnID, TurnID: providerTurnID}, err
	} else if found {
		existingTurnID = current.TurnID
		if !watcher.TurnImmutable(current.Status) {
			settledAt := time.Now().UTC()
			if _, _, err := w.turnStore.ApplyTurnFact(watcher.TurnFact{
				SessionID: current.SessionID, TurnID: current.TurnID,
				Class: watcher.EvidenceProvider, Kind: "done", Bound: true,
				SourceID:  "provider\x00test-host\x00" + current.TurnID + "\x00done",
				Admission: current.Admission, ActivityID: current.ActivityID,
				StartedAt: current.AcceptedAt, SettledAt: settledAt, At: settledAt,
			}); err != nil {
				return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: providerTurnID, TurnID: providerTurnID}, err
			}
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	pending, created, err := w.turnStore.PrepareInputAdmission(watcher.InputAdmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: providerTurnID,
		Receipt: providerTurnID, ClaimToken: claimToken, PayloadSHA256: digest,
		ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: acceptedAt.UTC(), Mode: watcher.InputAdmissionFresh, ExistingTurnID: existingTurnID,
	})
	if err != nil {
		return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: providerTurnID, TurnID: providerTurnID}, err
	}
	if !created {
		return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: providerTurnID, TurnID: providerTurnID},
			fmt.Errorf("Host submission was not freshly prepared")
	}
	resolvedAt := acceptedAt.Add(time.Millisecond).UTC()
	resolved, err := w.turnStore.ResolveInputAdmission(watcher.InputAdmissionResolution{
		SessionID: sessionID, ProposedTurnID: providerTurnID, Receipt: providerTurnID,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "host-activity-" + providerTurnID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-admission-" + providerTurnID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: providerTurnID, TurnID: providerTurnID}, err
	}
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: providerTurnID, TurnID: resolved.ResolvedTurnID}, nil
}
func (w *brainServiceTestWatcher) InputReceiptResult(_, receipt string) (watcher.InputResult, bool, error) {
	return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: receipt}, false, nil
}

func (w *brainServiceTestWatcher) KillSession(string) error {
	return nil
}

func (w *brainServiceTestWatcher) KillCompletedSession(string, string) error {
	return fmt.Errorf("completed Session cleanup not configured in this fixture")
}

func (w *brainServiceTestWatcher) CapturePaneContent(string) (string, error) {
	return "", nil
}

func (w *brainServiceTestWatcher) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	return watcher.ProviderActivityObservation{}, false, nil
}

func (w *brainServiceTestWatcher) ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	if w.GetWorker(sessionID) == nil {
		return watcher.OwnedGeneration{}, fmt.Errorf("Session %s is unavailable", sessionID)
	}
	return watcher.OwnedGeneration{
		SessionID:  sessionID,
		Generation: "test-owned-generation",
	}, nil
}

func (w *brainServiceTestWatcher) ResolveBrainHostGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	return w.ResolveOwnedGeneration(sessionID)
}

// killTrackingWatcher adds CreateSession and KillSession tracking needed for
// snapshot/live delegated coverage.
type killTrackingWatcher struct {
	brainServiceTestWatcher
	killed  []string
	created int
}

func (w *killTrackingWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	if w.sessions == nil {
		w.sessions = map[string]*classifier.Worker{}
	}
	w.created++
	id := fmt.Sprintf("mewla-worker-host:@%d", w.created)
	w.sessions[id] = &classifier.Worker{
		ID:      id,
		Name:    opts.Name,
		Cwd:     opts.Cwd,
		Command: opts.Command,
		State:   classifier.StateRunning,
		Hidden:  opts.Hidden,
	}
	return id, nil
}

func (w *killTrackingWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	delete(w.sessions, sessionID)
	return nil
}

func writeAndReadJSON(t *testing.T, conn *websocket.Conn, request clientMessage) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
