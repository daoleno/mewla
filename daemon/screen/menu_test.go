package screen

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMenuByName(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Active, apps[2].Active = false, true
	apps[0].Menu = []FakeMenuItem{
		{Path: "File > New Note", Char: "N", Adds: []Element{{Depth: 1, Role: "AXTextArea", Enabled: true}}},
		{Path: "File > Save As…"},
		{Path: "File > Print…", Char: "P", Disabled: true},
		{Path: "Edit > Find > Search", Char: "F", Modifiers: 2},
		{Path: "Notes > Quit Notes", Char: "Q"},
	}
	backend.RunningApps = apps
	act(t, s, "%7", AgentAction{Action: "apps"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()

	// The menu bar, then one menu, are listed.
	top := act(t, s, "%7", AgentAction{Action: "menu", App: "Notes"}).Text
	if !strings.Contains(top, "File") || !strings.Contains(top, "Edit") || strings.Contains(top, "  Save As") {
		t.Fatalf("menu bar:\n%s", top)
	}
	file := act(t, s, "%7", AgentAction{Action: "menu", App: "Notes", Text: "File >"}).Text
	if !strings.Contains(file, "New Note (cmd+n)") || !strings.Contains(file, "Print… (cmd+p) (turned off)") {
		t.Fatalf("File:\n%s", file)
	}
	if edit := act(t, s, "%7", AgentAction{Action: "menu", App: "Notes", Text: "Edit >"}).Text; !strings.Contains(edit, "Find >") {
		t.Fatalf("a submenu is marked:\n%s", edit)
	}
	// Choosing: case and the ellipsis don't matter; a name that isn't there is an error.
	if got := act(t, s, "%7", AgentAction{Action: "menu", App: "Notes", Text: "file > save as"}).Text; !strings.Contains(got, "Chose File > Save As…") {
		t.Fatal(got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "menu", App: "Notes", Text: "File > New Note"}).Text; !strings.Contains(got, "Chose File > New Note") {
		t.Fatal(got)
	}
	if !strings.Contains(act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text, "TextArea") {
		t.Fatal("the new note isn't there")
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "menu", App: "Notes", Text: "File > Print"}); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("turned off: %v", err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "menu", App: "Notes", Text: "File > Nope"}); err == nil {
		t.Fatal("a missing item must be an error")
	}
	if !slices.Equal(backend.MenuPresses, []string{"File > Save As…", "File > New Note"}) {
		t.Fatalf("menus %v", backend.MenuPresses)
	}
	// Quit is refused unless the person asked for it; keys are refused too.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "menu", App: "Notes", Text: "Notes > Quit Notes"}); err == nil || !strings.Contains(err.Error(), "ends an app") {
		t.Fatalf("quit: %v", err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", App: "Notes", Text: "cmd+q"}); err == nil || !strings.Contains(err.Error(), "quit the app") {
		t.Fatalf("cmd+q: %v", err)
	}
	if got := act(t, s, "%7", AgentAction{Action: "menu", App: "Notes", Text: "Notes > Quit Notes", ConfirmUserAsked: true}).Text; !strings.Contains(got, "Chose Notes > Quit Notes") {
		t.Fatal(got)
	}
}
