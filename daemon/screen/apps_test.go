package screen

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeApps() []FakeApp {
	return []FakeApp{
		{App: App{Name: "Notes", PID: 101, Active: true}, Elements: []Element{
			{Role: "AXWindow", Title: "Notes", Frame: image.Rect(0, 0, 2000, 1400), Enabled: true},
			{Depth: 1, Role: "AXButton", Title: "New Note", Frame: image.Rect(100, 100, 200, 140), Enabled: true, Actions: []string{"AXPress", "AXShowMenu"}},
			{Depth: 1, Role: "AXGroup", Enabled: true, Actions: []string{"AXScrollToVisible"}},
			{Depth: 1, Role: "AXSecureTextField", Title: "Password", Value: "hunter2", Frame: image.Rect(100, 200, 400, 240), Enabled: true, Actions: []string{"AXConfirm"}},
		}},
		{App: App{Name: "Safari", PID: 202, Hidden: true}, Elements: []Element{
			{Role: "AXWindow", Title: "Bank", Enabled: true},
			{Depth: 1, Role: "AXLink", Title: "Statements", Frame: image.Rect(-900, 0, -800, 20), Enabled: true, Actions: []string{"AXPress"}},
		}},
		{App: App{Name: "Helper", PID: 301}},
		{App: App{Name: "helper", PID: 302}},
	}
}

// Apps in the background are read without bringing them forward, each
// element with an id, and the handles behind the ids never pile up.
func TestAgentReadsAnyAppWithIds(t *testing.T) {
	s, backend, dir := newAgentService(t)
	backend.RunningApps = fakeApps()

	list := act(t, s, "%7", AgentAction{Action: "apps"}).Text
	if !strings.Contains(list, "Notes, pid 101, in front\n") || !strings.Contains(list, "Safari, pid 202, hidden\n") || strings.Index(list, "Notes") > strings.Index(list, "Safari") {
		t.Fatalf("apps:\n%s", list)
	}

	notes := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	for _, want := range []string{"App: Notes, pid 101, in front.", `[2] Button "New Note" {Press, ShowMenu} at (75, 60)`, `[4] SecureTextField "Password" {Confirm} at (125, 110)`} {
		if !strings.Contains(notes, want) {
			t.Fatalf("missing %q in:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "hunter2") || strings.Contains(notes, "[3]") {
		t.Fatalf("a password or an empty group shown:\n%s", notes)
	}

	safari := act(t, s, "%7", AgentAction{Action: "app_state", App: "safari"}).Text
	if !strings.Contains(safari, `[2] Link "Statements" {Press}`+"\n") {
		t.Fatalf("an element off the screen still has an id, without a point:\n%s", safari)
	}
	if backend.Retained != 2 {
		t.Fatalf("the earlier read's handles were kept: %d", backend.Retained)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "app_state", App: "HELPER"}); err == nil || !strings.Contains(err.Error(), "301, 302") {
		t.Fatalf("ambiguous name: %v", err)
	}
	if got := act(t, s, "%7", AgentAction{Action: "app_state", App: "302"}).Text; !strings.Contains(got, "App: helper, pid 302.") || !strings.Contains(got, "Nothing readable") {
		t.Fatalf("by pid:\n%s", got)
	}

	s.StopAgent()
	waitFor(t, func() bool {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		return backend.Retained == 0
	})
	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if !strings.Contains(string(audit), `"action":"app_state","app":"safari"`) {
		t.Fatalf("audit:\n%s", audit)
	}
}

// Acting by id works on an app behind others, refuses an element that has
// changed since it was read, and keeps what was set out of the audit.
func TestAgentActsOnElementsById(t *testing.T) {
	s, backend, dir := newAgentService(t)
	apps := fakeApps()
	apps[1].Elements = append(apps[1].Elements, Element{Depth: 1, Role: "AXTextField", Title: "Search", Enabled: true, Actions: []string{"AXConfirm"}})
	backend.RunningApps = apps
	element := func(id int, does, text string) (AgentResult, error) {
		return s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: id, ElementAction: does, Text: text})
	}
	if _, err := element(1, "press", ""); err == nil || !strings.Contains(err.Error(), "app_state first") {
		t.Fatalf("before reading: %v", err)
	}

	act(t, s, "%7", AgentAction{Action: "app_state", App: "Safari"})
	result, err := element(3, "set_value", "statements 2026")
	if err != nil || !strings.Contains(result.Text, "Done: set_value on [3]") || !strings.Contains(result.Text, `[3] TextField "Search" = "statements 2026"`) {
		t.Fatalf("set value: %v\n%s", err, result.Text)
	}
	if _, err := element(3, "Confirm", ""); err != nil {
		t.Fatalf("the element's own action: %v", err)
	}
	if _, err := element(2, "press", ""); err != nil {
		t.Fatalf("press: %v", err)
	}
	want := []FakeElementAction{{202, 2, "set_value", "statements 2026"}, {202, 2, "AXConfirm", ""}, {202, 1, "AXPress", ""}}
	if len(backend.ElementActions) != 3 || backend.ElementActions[0] != want[0] || backend.ElementActions[1] != want[1] || backend.ElementActions[2] != want[2] {
		t.Fatalf("actions %+v", backend.ElementActions)
	}
	if _, err := element(2, "delete_everything", ""); err == nil || !strings.Contains(err.Error(), "not something this element does") {
		t.Fatalf("unknown action: %v", err)
	}
	if _, err := element(9, "press", ""); err == nil || !strings.Contains(err.Error(), "no element [9]") {
		t.Fatalf("unknown id: %v", err)
	}

	// The page changed under the agent: the old id is refused, not guessed.
	backend.mu.Lock()
	backend.RunningApps[1].Elements[1].Title = "Transfer money"
	backend.mu.Unlock()
	if _, err := element(2, "press", ""); !errors.Is(err, ErrStaleElement) {
		t.Fatalf("stale: %v", err)
	}
	if len(backend.ElementActions) != 3 {
		t.Fatal("acted on a changed element")
	}

	// Someone at the computer pauses background actions too.
	act(t, s, "%7", AgentAction{Action: "app_state", App: "Safari"})
	backend.PersonInput()
	if _, err := element(2, "press", ""); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("person: %v", err)
	}

	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if strings.Contains(string(audit), "statements 2026") || !strings.Contains(string(audit), `"element":3,"element_action":"set_value","text_length":15`) {
		t.Fatalf("audit:\n%s", audit)
	}
}
