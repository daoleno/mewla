package watcher

import (
	"github.com/daoleno/mewla/daemon/classifier"
	"testing"
	"time"
)

func TestDelegatedApprovalCannotClaimRunning(t *testing.T) {
	now := time.Now()
	w := &classifier.Worker{LastProgressAt: &now, LeaseSeconds: 300}
	turn := TurnSnapshot{Status: TurnRunning, Summary: "working"}
	activity := classifier.NewCodexActivityAdapter().Infer(classifier.ActivityInput{PaneContent: "OpenAI Codex\nWould you like to run the following command?\n1. Yes\n2. No\nWorking (esc to interrupt)"})
	state, summary := projectDelegatedActivity(w, turn, activity)
	if state != classifier.StateBlocked || !w.NeedsAttention || w.LeaseSeconds != 0 || summary != "Waiting for Codex approval" {
		t.Fatalf("%s %s %+v", state, summary, w)
	}
	state, _ = projectDelegatedActivity(w, turn, classifier.ActivitySignal{})
	if state != classifier.StateRunning || w.NeedsAttention {
		t.Fatal("approval gate did not clear")
	}
	state, _ = projectDelegatedActivity(w, TurnSnapshot{Status: TurnDone}, activity)
	if state != classifier.StateDone {
		t.Fatal("approval overrode settlement")
	}
	state, _ = projectDelegatedTurn(w, TurnSnapshot{Status: TurnAdmitted})
	if state != classifier.StateUnknown {
		t.Fatal("unknown delivery reported as running")
	}
}

const (
	hostGateChoicePane     = "Claude Code\n←  ☐ Fruit  ✔ Submit  →\nWhich fruit?\n❯ 1. Apple\n  2. Type something.\n────\n  3. Chat about this\n\nEnter to select · Tab/Arrow keys to navigate · Esc to\ncancel\n"
	hostGatePlanPane       = "Claude Code\nClaude has written up a plan and is ready to execute. Would you like to proceed?\n❯ 1. Yes\n  2. No\n"
	hostGatePermissionPane = "Claude Code\nDo you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel · Tab to amend\n"
)

// The hidden Brain host only records review hand-offs in its ledger; the person
// chats with Brain without opening a turn. A settled host turn must not hide a
// prompt that is open in the pane right now.
func TestHostBlockedOverridesSettledTurn(t *testing.T) {
	adapter := classifier.NewClaudeActivityAdapter()
	cases := []struct {
		name    string
		pane    string
		summary string
	}{
		{"choice", hostGateChoicePane, "Waiting for your choice"},
		{"plan", hostGatePlanPane, "Waiting for plan approval"},
		{"permission", hostGatePermissionPane, "Waiting for Claude permission"},
	}
	for _, tc := range cases {
		for _, status := range []TurnStatus{TurnDone, TurnFailed, TurnUnknown} {
			t.Run(tc.name+"/"+string(status), func(t *testing.T) {
				host := &classifier.Worker{Hidden: true, Command: "claude"}
				turn := TurnSnapshot{Status: status}
				activity := adapter.Infer(classifier.ActivityInput{Worker: *host, PaneContent: tc.pane})
				state, summary := projectDelegatedActivity(host, turn, activity)
				if state != classifier.StateBlocked || summary != tc.summary ||
					host.Attention != "user_input" || !host.NeedsAttention {
					t.Fatalf("host gate hidden by settled turn: %s %q %+v", state, summary, host)
				}
				// Answering clears the gate: the settled projection and its
				// attention come back, nothing sticks at user_input.
				state, _ = projectDelegatedActivity(host, turn, classifier.ActivitySignal{})
				want, _ := projectDelegatedTurn(&classifier.Worker{}, turn)
				if state != want || host.Attention != "none" || host.NeedsAttention {
					t.Fatalf("host gate did not clear: %s %+v", state, host)
				}
			})
		}
	}
}

// A delegated Worker's settled turn stays final: only the hidden host may be
// blocked past settlement.
func TestWorkerSettledTurnIgnoresPaneGate(t *testing.T) {
	adapter := classifier.NewClaudeActivityAdapter()
	for _, pane := range []string{hostGateChoicePane, hostGatePlanPane, hostGatePermissionPane} {
		worker := &classifier.Worker{Command: "claude"}
		activity := adapter.Infer(classifier.ActivityInput{Worker: *worker, PaneContent: pane})
		if activity.State != classifier.StateBlocked {
			t.Fatalf("fixture is not a blocked pane: %+v", activity)
		}
		state, _ := projectDelegatedActivity(worker, TurnSnapshot{Status: TurnDone}, activity)
		if state != classifier.StateDone || worker.NeedsAttention || worker.Attention == "user_input" {
			t.Fatalf("settled Worker turn was overridden: %s %+v", state, worker)
		}
	}
}

// A host turn that is still open keeps today's behaviour.
func TestHostOpenTurnStillBlocksOnPaneGate(t *testing.T) {
	host := &classifier.Worker{Hidden: true, Command: "claude"}
	activity := classifier.NewClaudeActivityAdapter().Infer(classifier.ActivityInput{Worker: *host, PaneContent: hostGateChoicePane})
	state, _ := projectDelegatedActivity(host, TurnSnapshot{Status: TurnRunning}, activity)
	if state != classifier.StateBlocked || host.Attention != "user_input" {
		t.Fatalf("open host turn did not block: %s %+v", state, host)
	}
}
