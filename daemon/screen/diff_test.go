package screen

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A window of n buttons, numbered from 1 as a read gives ids.
func bigTree(n int) ElementTree {
	tree := ElementTree{App: "Big", PID: 5}
	tree.Elements = append(tree.Elements, Element{Role: "AXWindow", Title: "Main", Enabled: true})
	for i := 0; i < n; i++ {
		tree.Elements = append(tree.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("Item %d", i), Enabled: true, Actions: []string{"AXPress"}})
	}
	return tree
}

func rowsOf(tree ElementTree) []appRow {
	return appRows(tree, image.Rect(0, 0, 100, 100), func(p image.Point) (int, int) { return p.X, p.Y })
}

func diffOf(before, after ElementTree) (string, readStats, string, bool) {
	return diffAnswer(before, after, rowsOf(before), rowsOf(after))
}

func TestDiffNamesWhatChangedById(t *testing.T) {
	before := bigTree(40)
	before.Elements[1].Value = "old"
	after := bigTree(40)
	after.Elements[1].Value = "new"
	after.Elements[2].Title = "Renamed"
	after.Elements[3].Focused = true
	// Item 5 went away, and a sheet with a button came in after the window.
	after.Elements = append(after.Elements[:6], after.Elements[7:]...)
	after.Elements = append(after.Elements, Element{Depth: 1, Role: "AXSheet", Title: "Save?", Enabled: true}, Element{Depth: 2, Role: "AXButton", Title: "OK", Enabled: true})
	after.IDs = make([]int, len(after.Elements))
	for i := range after.IDs {
		after.IDs[i] = i + 1
		if i >= 6 && i < len(after.Elements)-2 {
			after.IDs[i] = i + 2 // ids stay with their elements across the gap
		}
	}
	after.IDs[len(after.IDs)-2], after.IDs[len(after.IDs)-1] = 101, 102

	text, stats, why, ok := diffOf(before, after)
	if !ok {
		t.Fatalf("a small change should be a difference: %s", why)
	}
	for _, want := range []string{
		`~ [2] Button "Item 0" = "new" {Press} (was = "old")`,
		`~ [3] Button "Renamed" {Press} (was "Item 1")`,
		`~ [4] Button "Item 2" {Press} (focused)`,
		`+ [101] Sheet "Save?"`,
		`+ [102] Button "OK"`,
		`- [7] Button "Item 5" {Press}`,
		"Focus: now [4] Button \"Item 2\".",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"Item 10"`) || stats.rows != 6 {
		t.Errorf("unchanged rows were listed, or miscounted (%d):\n%s", stats.rows, text)
	}
	full, _ := formatAppTree(App{Name: "Big", PID: 5}, after, image.Rect(0, 0, 100, 100), func(p image.Point) (int, int) { return p.X, p.Y })
	if len(text)*2 > len(full) {
		t.Errorf("the difference (%d bytes) isn't much smaller than the read (%d)", len(text), len(full))
	}
}

func TestDiffOfNothingSaysSo(t *testing.T) {
	tree := bigTree(30)
	tree.Elements[2].Focused = true
	text, stats, _, ok := diffOf(tree, tree)
	if !ok || stats.rows != 0 || !strings.Contains(text, "Nothing app_state shows changed.") || !strings.Contains(text, `Focus: still [3] Button "Item 1".`) {
		t.Fatalf("%v %v:\n%s", ok, stats, text)
	}
}

func TestFocusMovingAndLeaving(t *testing.T) {
	before, after := bigTree(30), bigTree(30)
	before.Elements[2].Focused = true
	after.Elements[7].Focused = true
	if text, _, _, _ := diffOf(before, after); !strings.Contains(text, `Focus: now [8] Button "Item 6" (was [3] Button "Item 1").`) {
		t.Fatalf("moved:\n%s", text)
	}
	after.Elements[7].Focused = false
	if text, _, _, _ := diffOf(before, after); !strings.Contains(text, `Focus: none now (was [3] Button "Item 1").`) {
		t.Fatalf("left:\n%s", text)
	}
}

// When a difference would mislead or save little, the whole read is given.
func TestDiffFallsBackToTheWholeRead(t *testing.T) {
	before := bigTree(40)
	window := func() ElementTree {
		after := bigTree(40)
		after.Elements = append(after.Elements, Element{Role: "AXWindow", Title: "Second", Enabled: true})
		return after
	}()
	if _, _, why, ok := diffOf(before, window); ok || !strings.Contains(why, "window") {
		t.Errorf("a new window: %v %q", ok, why)
	}

	cut := bigTree(40)
	cut.Truncated = true
	if _, _, why, ok := diffOf(before, cut); ok || !strings.Contains(why, "more than was read") {
		t.Errorf("a cut read: %v %q", ok, why)
	}
	if _, _, _, ok := diffOf(cut, before); ok {
		t.Error("an earlier cut read can't be compared either")
	}

	many := bigTree(40)
	for i := 1; i <= 25; i++ {
		many.Elements[i].Title += " changed"
	}
	if _, _, why, ok := diffOf(before, many); ok || !strings.Contains(why, "much of it changed") {
		t.Errorf("half changed: %v %q", ok, why)
	}

	other := bigTree(40)
	other.PID = 6
	if _, _, _, ok := diffOf(before, other); ok {
		t.Error("another app")
	}
}

// bigApp is a fake app of 40 buttons whose "Open" button shows a sheet, and
// whose "Window" button opens a second window.
func bigApp() FakeApp {
	app := FakeApp{App: App{Name: "Big", PID: 404, Active: true}}
	app.Elements = append(app.Elements, Element{Role: "AXWindow", Title: "Main", Enabled: true},
		Element{Depth: 1, Role: "AXButton", Title: "Open", Enabled: true, Actions: []string{"AXPress"}},
		Element{Depth: 1, Role: "AXButton", Title: "Window", Enabled: true, Actions: []string{"AXPress"}},
		Element{Depth: 1, Role: "AXTextField", Title: "Name", Enabled: true, Actions: []string{"AXConfirm"}})
	for i := 0; i < 40; i++ {
		app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("Item %d", i), Enabled: true, Actions: []string{"AXPress"}})
	}
	return app
}

func TestElementActionsAnswerWithWhatChanged(t *testing.T) {
	s, backend, dir := newAgentService(t)
	backend.RunningApps = []FakeApp{bigApp()}
	backend.OnElementAction = func(app *FakeApp, index int, action string) {
		switch app.Elements[index].Title {
		case "Open":
			app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXSheet", Title: "Save changes?", Enabled: true},
				Element{Depth: 2, Role: "AXButton", Title: "Discard", Enabled: true, Actions: []string{"AXPress"}})
		case "Window":
			app.Elements = append(app.Elements, Element{Role: "AXWindow", Title: "Second", Enabled: true},
				Element{Depth: 1, Role: "AXButton", Title: "Close", Enabled: true, Actions: []string{"AXPress"}})
		}
	}
	state := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	press := func(id int) AgentResult {
		return act(t, s, "%7", AgentAction{Action: "element", Element: id, ElementAction: "press"})
	}

	// A sheet opens: its elements are in the answer, and little else.
	opened := press(idOf(t, state, `Button "Open"`))
	if !strings.Contains(opened.Text, `+ [`) || !strings.Contains(opened.Text, `Sheet "Save changes?"`) || !strings.Contains(opened.Text, `Button "Discard"`) {
		t.Fatalf("the sheet is missing:\n%s", opened.Text)
	}
	if strings.Contains(opened.Text, "Item 7") || len(opened.Text)*3 > len(state) || !opened.diff {
		t.Fatalf("the answer isn't a difference (%d bytes of %d):\n%s", len(opened.Text), len(state), opened.Text)
	}

	// The read it left behind is whole: an element it didn't repeat, and one
	// it named, are both acted on by id, and matched.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "expect", Match: &ElementMatch{Role: "Button", Title: "Item 7"}}); err != nil {
		t.Fatalf("an unchanged row is not in the latest read: %v", err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Match: &ElementMatch{Role: "Button", Title: "Discard"}, ElementAction: "press"}); err != nil {
		t.Fatalf("the new button: %v", err)
	}

	// A window opens: the whole app is given, with the new window in it.
	window := press(idOf(t, state, `Button "Window"`))
	if window.diff || !strings.Contains(window.Text, "The whole app now (a window opened or closed)") || !strings.Contains(window.Text, `Window "Second"`) || !strings.Contains(window.Text, `Button "Close"`) {
		t.Fatalf("a new window should give the whole app:\n%s", window.Text)
	}

	// Setting a value shows that one row.
	set := act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, state, `TextField "Name"`), ElementAction: "set_value", Text: "Ada"})
	if !set.diff || !strings.Contains(set.Text, `~ [`) || !strings.Contains(set.Text, `TextField "Name" = "Ada"`) || strings.Contains(set.Text, "Item 7") {
		t.Fatalf("set_value:\n%s", set.Text)
	}

	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if !strings.Contains(string(audit), `"diff":true`) || strings.Contains(string(audit), "Ada") {
		t.Fatalf("audit:\n%s", audit)
	}
}
