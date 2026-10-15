package screen

import (
	"context"
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
