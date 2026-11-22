package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// The Session's own line says what happened to its agent: restarting while it
// starts again, restarted until the agent shows something newer, not
// restarted while the pane stays dead.
func TestAgentRelaunchLine(t *testing.T) {
	now := time.Date(2026, 11, 22, 1, 0, 0, 0, time.UTC)
	s := newAgentRelaunchSupervisor()

	s.setLine("a", agentRelaunchLine{kind: agentLineRestarting, text: restartingLine("Restarted after SIGKILL")})
	if state, line, ok := s.line("a", false, classifier.StateFailed, now); !ok || state != classifier.StateRunning ||
		line != (classifier.Relaunch{State: "restarting", Line: "Restarting after SIGKILL"}) {
		t.Fatalf("restarting = %v %+v %v", state, line, ok)
	}

	s.setLine("a", agentRelaunchLine{kind: agentLineRestarted, text: "Restarted after SIGKILL", until: now.Add(agentRelaunchShown)})
	if state, line, ok := s.line("a", true, classifier.StateDone, now); !ok || state != classifier.StateDone ||
		line != (classifier.Relaunch{State: "restarted", Line: "Restarted after SIGKILL"}) {
		t.Fatalf("restarted idle = %v %+v %v", state, line, ok)
	}
	if _, _, ok := s.line("a", true, classifier.StateRunning, now); ok {
		t.Fatal("the restarted line hid the agent's own work")
	}
	if _, _, ok := s.line("a", true, classifier.StateDone, now); ok {
		t.Fatal("the restarted line came back after the agent showed work")
	}

	s.setLine("b", agentRelaunchLine{kind: agentLineRestarted, text: "Restarted after exit 1", until: now.Add(agentRelaunchShown)})
	if _, _, ok := s.line("b", true, classifier.StateDone, now.Add(agentRelaunchShown+time.Second)); ok {
		t.Fatal("the restarted line outlived its time")
	}

	s.setLine("c", agentRelaunchLine{kind: agentLineStopped, text: "Gave up after 3 restarts"})
	if state, line, ok := s.line("c", false, classifier.StateDone, now); !ok || state != classifier.StateFailed ||
		line != (classifier.Relaunch{State: "not_restarted", Line: "Gave up after 3 restarts"}) {
		t.Fatalf("stopped = %v %+v %v", state, line, ok)
	}
	if _, _, ok := s.line("c", true, classifier.StateDone, now); ok {
		t.Fatal("the not-restarted line stayed on a live agent")
	}

	s.setLine("d", agentRelaunchLine{kind: agentLineStopped})
	if _, _, ok := s.line("d", false, classifier.StateDone, now); ok {
		t.Fatal("an empty line was shown")
	}
}

func TestAgentEndShortCause(t *testing.T) {
	code := func(n int) *int { return &n }
	for _, test := range []struct {
		end  AgentEnd
		want string
	}{
		{AgentEnd{Exit: SessionExit{Signal: "KILL"}}, "SIGKILL"},
		{AgentEnd{Exit: SessionExit{Signal: "SIGKILL"}, OutOfMemory: true}, "running out of memory"},
		{AgentEnd{Exit: SessionExit{Code: code(1)}}, "exit 1"},
		{AgentEnd{}, "it ended"},
	} {
		if got := test.end.ShortCause(); got != test.want {
			t.Fatalf("%+v = %q, want %q", test.end, got, test.want)
		}
	}
}
