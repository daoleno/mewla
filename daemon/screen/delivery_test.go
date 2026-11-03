package screen

import (
	"context"
	"strings"
	"testing"
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
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: field, ElementAction: "set_value", Text: "new"}).Text; !strings.Contains(got, "Delivery: verified, the element now holds it") {
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
