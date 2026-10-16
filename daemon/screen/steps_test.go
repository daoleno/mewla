package screen

import (
	"context"
	"fmt"
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

// noteRecipe makes a note with text in one call: choose New Note by its
// shortcut, wait for the new note's empty text, write it, read it back.
// The empty match keeps a shortcut that didn't take from writing over the
// note on show.
func noteRecipe(text string) []AgentAction {
	return []AgentAction{
		{Action: "key", App: "Notes", Text: "cmd+n"},
		{Action: "wait_for", App: "Notes", Match: &ElementMatch{Role: "TextArea", Empty: true}, Duration: 1},
		{Action: "element", Match: &ElementMatch{Role: "TextArea", Empty: true}, ElementAction: "set_value", Text: text},
		{Action: "element", Match: &ElementMatch{Role: "TextArea", Value: strings.SplitN(text, "\n", 2)[0]}, ElementAction: "read"},
	}
}

func notesBehind() []FakeApp {
	apps := fakeApps()
	apps[0].Active, apps[2].Active = false, true
	apps[0].Elements = append(apps[0].Elements, Element{Depth: 1, Role: "AXTextArea", Value: "Shopping list\nmilk", Enabled: true})
	return apps
}

// A note with text takes one call, behind the app someone is using, and is
// read back whole; native Codex took 14 calls for the same on a Mac.
func TestANoteWithTextInOneCall(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := notesBehind()
	apps[0].Menu = []FakeMenuItem{{Path: "File > New Note", Char: "N", Adds: []Element{{Depth: 1, Role: "AXTextArea", Enabled: true}}}}
	backend.RunningApps = apps
	act(t, s, "%7", AgentAction{Action: "apps"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()

	text := "Mewla check\nV2 computer benchmark\nVerification line 3 v2"
	result, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: noteRecipe(text)})
	if err != nil || !strings.HasPrefix(result.Text, "1. key: done\n2. wait_for: done\n3. element: done\n4. element: done\n") ||
		!strings.HasSuffix(result.Text, fmt.Sprintf("TextArea holds %d characters:\n%s", len(text), text)) {
		t.Fatalf("recipe: %v\n%q", err, result.Text)
	}
	if len(backend.AppKeys) != 0 || len(backend.MenuPresses) != 1 {
		t.Fatalf("keys %v, menus %v", backend.AppKeys, backend.MenuPresses)
	}
	if notes := backend.RunningApps[0].Elements; notes[len(notes)-2].Value != "Shopping list\nmilk" || notes[len(notes)-1].Value != text {
		t.Fatalf("notes %+v", notes)
	}
}

// When the shortcut doesn't make a new note, the recipe stops before
// writing: the note on show is never written over.
func TestANoteRecipeNeverWritesOverANote(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = notesBehind() // no menu: the keys go, and are ignored
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: noteRecipe("Mewla check\nx")})
	if err == nil || !strings.Contains(err.Error(), "2. wait_for: failed") || !strings.Contains(err.Error(), "did not run") {
		t.Fatalf("recipe: %v", err)
	}
	for _, a := range backend.ElementActions {
		if a.Action == "set_value" {
			t.Fatalf("wrote %+v", a)
		}
	}
	if notes := backend.RunningApps[0].Elements; notes[len(notes)-1].Value != "Shopping list\nmilk" {
		t.Fatalf("the note on show changed: %+v", notes[len(notes)-1])
	}
}
