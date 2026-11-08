package screen

import (
	"strings"
	"testing"
)

func popupApp(active bool) []FakeApp {
	return []FakeApp{
		{App: App{Name: "Person", PID: 100, Active: !active}, Elements: []Element{{Role: "AXWindow", Title: "Person", Enabled: true}}},
		{App: App{Name: "Tickets", PID: 501, Active: active}, Elements: []Element{
			{Role: "AXWindow", Title: "Tickets", Enabled: true},
			{Depth: 1, Role: "AXPopUpButton", Title: "Team", Value: "Choose", Enabled: true, Actions: []string{"AXPress"}},
		}},
	}
}

func TestAPopUpIsChosenInOneStepFromAnAppBehind(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = popupApp(false)
	backend.PopupItems = map[string][]string{"Team": {"Choose", "Platform", "Growth"}}
	backend.GuardTakes = 1
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Tickets"}).Text
	if !strings.Contains(state, "choose an item with set_value") {
		t.Fatalf("the pop-up's row doesn't say how to choose: %s", state)
	}
	id := idOf(t, state, `PopUpButton "Team"`)
	got := act(t, s, "%7", AgentAction{Action: "element", Element: id, ElementAction: "set_value", Text: "growth"}).Text
	for _, want := range []string{"Delivery: verified", "Chose \"Growth\" from the pop-up in one step", "kept it with Person"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in: %s", want, got)
		}
	}
	if len(backend.PopupChoices) != 1 || backend.PopupChoices[0].Item != "Growth" {
		t.Fatalf("choices %+v", backend.PopupChoices)
	}
	if len(backend.KeyGuards) != 1 || !backend.KeyGuards[0].Ended || backend.KeyGuards[0].Front != 100 || backend.KeyGuards[0].Target != 501 {
		t.Fatalf("guards %+v", backend.KeyGuards)
	}
}

func TestAnUnknownPopUpItemListsTheItemsAndChoosesNothing(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = popupApp(false)
	backend.PopupItems = map[string][]string{"Team": {"Choose", "Platform", "Growth"}}
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Tickets"}).Text
	id := idOf(t, state, `PopUpButton "Team"`)
	_, err := s.Act(t.Context(), "%7", AgentAction{Action: "element", Element: id, ElementAction: "set_value", Text: "Sales"})
	if err == nil || !strings.Contains(err.Error(), "Platform, Growth") || !strings.Contains(err.Error(), "safe to retry") {
		t.Fatalf("error %v", err)
	}
	if len(backend.PopupChoices) != 0 || len(backend.KeyGuards) != 1 || !backend.KeyGuards[0].Ended {
		t.Fatalf("choices %v guards %+v", backend.PopupChoices, backend.KeyGuards)
	}
}

func TestNoGuardIsArmedForTheAppInFront(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = popupApp(true)
	backend.PopupItems = map[string][]string{"Team": {"Choose", "Platform"}}
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Tickets"}).Text
	id := idOf(t, state, `PopUpButton "Team"`)
	got := act(t, s, "%7", AgentAction{Action: "element", Element: id, ElementAction: "set_value", Text: "Platform"}).Text
	if len(backend.KeyGuards) != 0 || strings.Contains(got, "kept it") {
		t.Fatalf("guards %+v: %s", backend.KeyGuards, got)
	}
}

func TestAGuardThatCannotBeArmedChangesNothing(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = popupApp(false)
	backend.PopupItems = map[string][]string{"Team": {"Choose", "Platform"}}
	backend.NoKeyGuard = true
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Tickets"}).Text
	id := idOf(t, state, `PopUpButton "Team"`)
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: id, ElementAction: "set_value", Text: "Platform"}).Text; !strings.Contains(got, "Chose \"Platform\"") {
		t.Fatal(got)
	}
}

func TestMatchPopupItem(t *testing.T) {
	opts := []string{"Open", "Save As…", "Close"}
	for want, at := range map[string]int{"open": 0, "Save As": 2 - 1, "CLOSE": 2, "Print": -1} {
		if got := matchPopupItem(opts, want); got != at {
			t.Errorf("%q: %d want %d", want, got, at)
		}
	}
}

func TestPressingAPopUpPointsToSetValue(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = popupApp(false)
	backend.PopupItems = map[string][]string{"Team": {"Choose", "Platform"}}
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Tickets"}).Text
	id := idOf(t, state, `PopUpButton "Team"`)
	got := act(t, s, "%7", AgentAction{Action: "element", Element: id, ElementAction: "press"}).Text
	if !strings.Contains(got, "Choose with set_value and the item's title") || len(backend.KeyGuards) != 1 {
		t.Fatalf("guards %d: %s", len(backend.KeyGuards), got)
	}
}
