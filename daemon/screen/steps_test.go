package screen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Find, act, type, check, in one call: each step through the same gates,
// with its own audit line, stopping at the first that fails.
func TestAgentRunsStepsInOneCall(t *testing.T) {
	s, backend, dir := newAgentService(t)
	apps := fakeApps()
	apps[1].Elements = append(apps[1].Elements,
		Element{Depth: 1, Role: "AXTextField", Title: "Search", Enabled: true},
		Element{Depth: 1, Role: "AXButton", Title: "Go", Enabled: true, Actions: []string{"AXPress"}},
		Element{Depth: 1, Role: "AXButton", Title: "Go back", Enabled: true, Actions: []string{"AXPress"}},
	)
	backend.RunningApps = apps
	steps := []AgentAction{
		{Action: "wait_for", App: "Safari", Match: &ElementMatch{Role: "TextField", Title: "search"}},
		{Action: "element", Match: &ElementMatch{Title: "Search"}, ElementAction: "set_value", Text: "rent"},
		{Action: "element", Match: &ElementMatch{Role: "AXButton", Title: "go"}, ElementAction: "press"},
		{Action: "expect", App: "Safari", Match: &ElementMatch{Title: "Search", Value: "rent"}},
	}
	result, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: steps})
	if err != nil || !strings.HasPrefix(result.Text, "1. wait_for: done\n2. element: done\n3. element: done\n4. expect: done\n") || !strings.Contains(result.Text, "As expected: [3]") {
		t.Fatalf("steps: %v\n%s", err, result.Text)
	}
	// "go" is a whole name for one button and part of another: the whole wins.
	if a := backend.ElementActions; len(a) != 2 || a[1].Index != 3 {
		t.Fatalf("actions %+v", a)
	}

	// A step that fails stops the batch, and the batch fails.
	_, err = s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: []AgentAction{
		{Action: "expect", Match: &ElementMatch{Title: "Transfer"}},
		{Action: "element", Match: &ElementMatch{Title: "Go"}, ElementAction: "press"},
	}})
	if err == nil || !strings.Contains(err.Error(), "1. expect: failed") || !strings.Contains(err.Error(), "did not run") || len(backend.ElementActions) != 2 {
		t.Fatalf("failed step: %v", err)
	}
	// Ambiguity is refused, not guessed, when acting.
	act(t, s, "%7", AgentAction{Action: "app_state", App: "Safari"})
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Match: &ElementMatch{Role: "Button"}, ElementAction: "press"}); err == nil || !strings.Contains(err.Error(), "name one by id") {
		t.Fatalf("ambiguous: %v", err)
	}

	start := time.Now()
	_, err = s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: []AgentAction{
		{Action: "wait_for", App: "Safari", Match: &ElementMatch{Title: "Never"}, Duration: 1},
	}})
	if err == nil || !strings.Contains(err.Error(), "did not appear within 1s") || time.Since(start) > 3*time.Second {
		t.Fatalf("wait_for: %v after %s", err, time.Since(start))
	}
	tooMany := make([]AgentAction, maxSteps+1)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: tooMany}); err == nil {
		t.Fatal("too many steps")
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: []AgentAction{{Action: "steps"}}}); err == nil {
		t.Fatal("nested steps")
	}

	// Someone at the computer stops the batch at the next step on the app
	// in front.
	backend.PersonInput()
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: []AgentAction{{Action: "app_state", App: "Notes"}}}); err == nil || !strings.Contains(err.Error(), ErrPersonAtComputer.Error()) {
		t.Fatalf("person: %v", err)
	}

	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if strings.Contains(string(audit), `"rent"`) || strings.Count(string(audit), `"action":"element"`) < 3 {
		t.Fatalf("audit:\n%s", audit)
	}
}
