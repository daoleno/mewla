package screen

import (
	"context"
	"strings"
	"testing"
	"time"
)

func checkboxApp(stubborn bool) []FakeApp {
	return []FakeApp{{App: App{Name: "Settings", PID: 501, Active: true}, Elements: []Element{
		{Role: "AXWindow", Title: "Settings", Enabled: true},
		{Depth: 1, Role: "AXCheckBox", Title: "Sync", Value: "0", Enabled: true, Actions: []string{"AXPress"}},
		{Depth: 1, Role: "AXTextField", Title: "Name", Value: "old", Enabled: true},
	}}}
}

func TestCheckUncheckClearAreIdempotentAndSayWhetherDelivered(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = checkboxApp(false)
	backend.OnElementAction = func(app *FakeApp, index int, action string) {
		if action == "AXPress" && app.Elements[index].Role == "AXCheckBox" {
			if app.Elements[index].Value == "0" {
				app.Elements[index].Value = "1"
			} else {
				app.Elements[index].Value = "0"
			}
		}
	}
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Settings"}).Text
	box, field := idOf(t, state, `CheckBox "Sync"`), idOf(t, state, `TextField "Name"`)

	if got := act(t, s, "%7", AgentAction{Action: "element", Element: box, ElementAction: "check"}).Text; !strings.Contains(got, "Delivery: verified, it is now checked") {
		t.Fatalf("check: %s", got)
	}
	// Again: nothing is pressed, so it doesn't undo itself.
	n := len(backend.ElementActions)
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: box, ElementAction: "check"}).Text; !strings.Contains(got, "Nothing to do") || len(backend.ElementActions) != n {
		t.Fatalf("second check: %s (%d actions)", got, len(backend.ElementActions))
	}
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: box, ElementAction: "uncheck"}).Text; !strings.Contains(got, "verified, it is now unchecked") {
		t.Fatalf("uncheck: %s", got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: field, ElementAction: "set_value", Text: "new"}).Text; !strings.Contains(got, "Delivery: verified, it holds it") {
		t.Fatalf("set_value: %s", got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: field, ElementAction: "clear"}).Text; !strings.Contains(got, "Delivery: verified") {
		t.Fatalf("clear: %s", got)
	}
	if v, _, _ := backend.ReadValue(context.Background(), Element{Role: "AXTextField", Title: "Name", ref: uintptr(501<<16 | 3)}, 10); v != "" {
		t.Fatalf("value after clear %q", v)
	}
}

func TestADeliveryThatDidNotTakeIsSaidSo(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = checkboxApp(true) // pressing changes nothing
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Settings"}).Text
	box := idOf(t, state, `CheckBox "Sync"`)
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: box, ElementAction: "check"}).Text; !strings.Contains(got, "still unchecked") {
		t.Fatalf("a refused check: %s", got)
	}
}

func TestWaitForGoneAndEnabled(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = checkboxApp(false)
	backend.RunningApps[0].Elements[1].Enabled = false
	// Not gone while it is there; gone once it isn't; enabled once it is.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "wait_for", App: "Settings", Until: "gone", Match: &ElementMatch{Role: "CheckBox"}, Duration: 1}); err == nil || !strings.Contains(err.Error(), "did not go away") {
		t.Fatalf("gone while present: %v", err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "wait_for", App: "Settings", Until: "enabled", Match: &ElementMatch{Role: "CheckBox"}, Duration: 1}); err == nil || !strings.Contains(err.Error(), "did not become enabled") {
		t.Fatalf("enabled while disabled: %v", err)
	}
	backend.RunningApps[0].Elements[1].Enabled = true
	if got := act(t, s, "%7", AgentAction{Action: "wait_for", App: "Settings", Until: "enabled", Match: &ElementMatch{Role: "CheckBox"}, Duration: 1}).Text; !strings.HasPrefix(got, "Found [") || !strings.Contains(got, "enabled") || strings.Contains(got, "TextField") {
		t.Fatalf("enabled: %s", got)
	}
	backend.RunningApps[0].Elements = backend.RunningApps[0].Elements[:1]
	if got := act(t, s, "%7", AgentAction{Action: "wait_for", App: "Settings", Until: "gone", Match: &ElementMatch{Role: "CheckBox"}, Duration: 1}).Text; !strings.Contains(got, "is gone") {
		t.Fatalf("gone: %s", got)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "wait_for", App: "Settings", Until: "sometime", Match: &ElementMatch{Role: "CheckBox"}}); err == nil {
		t.Fatal("an unknown until must be refused")
	}
}

// An action whose effect shows at once is answered after one read, with no
// wait; one that shows nothing is looked at again after the longer wait.
func TestElementActionReadsOnceWhenItShowedAtOnceAndTwiceWhenNot(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = checkboxApp(false)
	backend.OnElementAction = func(app *FakeApp, index int, action string) {
		if action == "AXPress" {
			app.Elements[index].Value = "1"
		}
	}
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Settings"}).Text
	box := idOf(t, state, `CheckBox "Sync"`)
	reads := backend.Reads
	start := time.Now()
	act(t, s, "%7", AgentAction{Action: "element", Element: box, ElementAction: "press"})
	if got := backend.Reads - reads; got != 1 {
		t.Fatalf("a visible change took %d reads", got)
	}
	if d := time.Since(start); d > settleFirst {
		t.Fatalf("a visible change waited %v", d)
	}
	// Focusing a field changes the focus row at once too; pressing something
	// that changes nothing is read twice.
	reads = backend.Reads
	backend.OnElementAction = nil
	act(t, s, "%7", AgentAction{Action: "element", Element: box, ElementAction: "press"})
	if got := backend.Reads - reads; got != 2 {
		t.Fatalf("no visible change took %d reads", got)
	}
}
