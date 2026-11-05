package watcher

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

// The 2026-11-05 incident, end to end on the real Claude 2.1.318 captures in
// ../classifier/testdata/claude-2.1.318: Brain was told of an open permission
// dialog with no usable id and `mewla worker answer` refused with
// prompt_refused. The event, the Worker's status and the answer must all rest
// on one id, and a dialog that changed under the id must not get the key.

func realClaudeDialog(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../classifier/testdata/claude-2.1.318/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// screenPane is a Claude pane showing one fixed screen: the visible capture
// and the exact dialog capture are the same alternate screen, as on a real
// pane. A key answers it (the dialog goes away) and is recorded.
type screenPane struct {
	screen string
	keys   []string
}

func (p *screenPane) capture() (string, error) { return p.screen, nil }
func (p *screenPane) captureDialog() (string, classifier.CaptureInfo, error) {
	return p.screen, classifier.CaptureInfo{Exact: true, Alternate: true}, nil
}
func (p *screenPane) sendKey(key string) error {
	p.keys = append(p.keys, key)
	p.screen = "● Done\n\n❯ \n"
	return nil
}
func (p *screenPane) sendText(string) error { return nil }

func delegatedClaude(t *testing.T) (*Watcher, string, classifier.Worker, *fakeTurnLedger, TurnSnapshot) {
	t.Helper()
	w, sessionID := watcherWithClaude(t)
	worker := *w.workers[sessionID]
	ledger := newFakeTurnLedger()
	turn := TurnSnapshot{SessionID: sessionID, TurnID: "turn:real", Status: TurnRunning, SignalProtocol: true}
	ledger.seed(sessionID, turn)
	w.SetTurnLedger(ledger)
	return w, sessionID, worker, ledger, turn
}

func TestRealClaudeDialogEventStatusAndAnswerShareOneID(t *testing.T) {
	for _, name := range []string{"bash-multiline-ssh.txt", "dangerous-rm.txt"} {
		w, sessionID, worker, ledger, turn := delegatedClaude(t)
		pane := &screenPane{screen: realClaudeDialog(t, name)}

		// The poll: the exact capture gives the signal the gate and the status read.
		text, info, _ := pane.captureDialog()
		activity := w.activitySignalCaptured(worker, text, &info)
		if activity.State != classifier.StateBlocked || activity.Prompt == nil {
			t.Fatalf("%s: the poll found no prompt (%q): %+v", name, activity.Unidentified, activity)
		}
		id := activity.Prompt.ID

		// The event Brain reads names that id, and the command that answers it.
		w.syncPromptGate(sessionID, worker, true, activity, turn, true, nil)
		if len(ledger.applied) != 1 {
			t.Fatalf("%s: applied %+v", name, ledger.applied)
		}
		var details struct {
			Prompt             classifier.Prompt `json:"prompt"`
			AnswerWith         string            `json:"answer_with"`
			PromptUnidentified string            `json:"prompt_unidentified"`
		}
		if err := json.Unmarshal([]byte(ledger.applied[0].DetailsJSON), &details); err != nil {
			t.Fatal(err)
		}
		if details.Prompt.ID != id || !strings.Contains(details.AnswerWith, "-prompt "+id+" ") || details.PromptUnidentified != "" {
			t.Fatalf("%s: event %+v, want id %s", name, details, id)
		}

		// The answer-time read is the same id.
		current, err := w.currentPrompt(pane, sessionID)
		if err != nil || current == nil || current.ID != id {
			t.Fatalf("%s: answer-time prompt %+v (%v), want %s", name, current, err, id)
		}
		if err := w.answerPrompt(pane, sessionID, id, 1); err != nil {
			t.Fatalf("%s: answer: %v", name, err)
		}
		if len(pane.keys) != 1 || pane.keys[0] != "1" {
			t.Fatalf("%s: keys %v", name, pane.keys)
		}
	}
}

// Stale: Brain composed its answer for one dialog; by the time it is sent the
// pane shows another (one character of the command differs). The key goes to
// nobody.
func TestRealClaudeStaleDialogGetsNoKey(t *testing.T) {
	w, sessionID, worker, _, _ := delegatedClaude(t)
	original := realClaudeDialog(t, "bash-multiline-ssh.txt")
	first := &screenPane{screen: original}
	text, info, _ := first.captureDialog()
	activity := w.activitySignalCaptured(worker, text, &info)
	if activity.Prompt == nil {
		t.Fatalf("no prompt: %+v", activity)
	}
	lines := strings.Split(original, "\n")
	changed := strings.Join(lines[:17], "\n") + "\n" + strings.Replace(strings.Join(lines[17:], "\n"), "systemctl status foo", "systemctl status fob", 1)
	if changed == original {
		t.Fatal("fixture did not change")
	}
	stale := &screenPane{screen: changed}
	err := w.answerPrompt(stale, sessionID, activity.Prompt.ID, 1)
	if !errors.Is(err, ErrPromptGone) || len(stale.keys) != 0 {
		t.Fatalf("a different dialog was answered with the old id: err=%v keys=%v", err, stale.keys)
	}
	// The dialog closed altogether.
	gone := &screenPane{screen: "● Done\n\n❯ \n"}
	if err := w.answerPrompt(gone, sessionID, activity.Prompt.ID, 1); !errors.Is(err, ErrPromptGone) || len(gone.keys) != 0 {
		t.Fatalf("a closed dialog got a key: err=%v keys=%v", err, gone.keys)
	}
	// An option the dialog does not have.
	same := &screenPane{screen: original}
	if err := w.answerPrompt(same, sessionID, activity.Prompt.ID, 4); err == nil || len(same.keys) != 0 {
		t.Fatalf("option 4 of a 3-option dialog: err=%v keys=%v", err, same.keys)
	}
}

// A request taller than the pane has no id: the event says so and why instead
// of leaving an id-shaped string, the answer is refused with the same reason,
// and a permission hook that holds the decision keeps the event to itself.
func TestRealClaudeTallDialogIsReportedUnidentifiedNotWithAFakeID(t *testing.T) {
	w, sessionID, worker, ledger, turn := delegatedClaude(t)
	pane := &screenPane{screen: realClaudeDialog(t, "tall-60-lines-40x40.txt")}
	text, info, _ := pane.captureDialog()
	activity := w.activitySignalCaptured(worker, text, &info)
	if activity.State != classifier.StateBlocked || activity.Prompt != nil || activity.Unidentified == "" {
		t.Fatalf("tall dialog: %+v", activity)
	}

	held := true
	w.SetApprovalHold(func(string) bool { return held })
	w.syncPromptGate(sessionID, worker, true, activity, turn, true, nil)
	if len(ledger.applied) != 0 {
		t.Fatalf("the pane filed a dialog whose hook holds the exact request: %+v", ledger.applied)
	}

	held = false
	w.syncPromptGate(sessionID, worker, true, activity, turn, true, nil)
	if len(ledger.applied) != 1 {
		t.Fatalf("applied %+v", ledger.applied)
	}
	if !strings.Contains(ledger.applied[0].SourceID, "pane-gate:turn:real:unidentified:") {
		t.Fatalf("the event id does not say the dialog is unidentified: %s", ledger.applied[0].SourceID)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(ledger.applied[0].DetailsJSON), &details); err != nil {
		t.Fatal(err)
	}
	if reason, _ := details["prompt_unidentified"].(string); !strings.Contains(reason, "where the dialog starts") {
		t.Fatalf("no reason in the event: %v", details)
	}
	if _, has := details["answer_with"]; has {
		t.Fatalf("an answer command for a dialog with no id: %v", details)
	}
	if next, _ := details["next"].(string); !strings.Contains(next, "worker approvals") {
		t.Fatalf("no next step: %v", details)
	}

	_, err := w.currentPrompt(pane, sessionID)
	if err == nil || !errors.Is(err, ErrClaudeChoiceRefused) || !strings.Contains(err.Error(), "where the dialog starts") || len(pane.keys) != 0 {
		t.Fatalf("answer-time refusal: %v keys=%v", err, pane.keys)
	}
}
