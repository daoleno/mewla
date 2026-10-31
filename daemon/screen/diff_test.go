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
	return diffAnswer(before, after, rowsOf(before), rowsOf(after), 0)
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

// A note: a window with a few buttons, and a text area whose lines are the
// elements under it, so editing the text rebuilds all of them.
func noteTree(lines int, text string, firstLineID int) ElementTree {
	tree := bigTree(5)
	tree.Elements = append(tree.Elements, Element{Depth: 1, Role: "AXTextArea", Title: "Note", Value: text, Enabled: true, Focused: true})
	for i := 0; i < lines; i++ {
		tree.Elements = append(tree.Elements, Element{Depth: 2, Role: "AXStaticText", Value: fmt.Sprintf("%s line %d", text, i), Enabled: true})
	}
	tree.IDs = make([]int, len(tree.Elements))
	for i := range tree.IDs {
		tree.IDs[i] = i + 1
		if i > 6 {
			tree.IDs[i] = firstLineID + i + 1 // the lines of a rebuilt text area are new elements
		}
	}
	return tree
}

func TestDiffCountsWhatChangedUnderTheActedElementWithIt(t *testing.T) {
	before := noteTree(60, "old", 0)
	after := noteTree(70, "new", 1000)
	textArea := before.id(6)

	// Without being told what was acted on, the rebuilt lines are "much of it".
	if _, _, why, ok := diffOf(before, after); ok || !strings.Contains(why, "much of it changed") {
		t.Fatalf("every line changed: %v %q", ok, why)
	}

	text, stats, why, ok := diffAnswer(before, after, rowsOf(before), rowsOf(after), textArea)
	if !ok {
		t.Fatalf("a rebuilt subtree under the edited element is one change: %s", why)
	}
	for _, want := range []string{
		`~ [7] TextArea "Note" = "new" (focused)`,
		`(was = "old")`,
		"rows inside it changed with it",
		"Focus: still [7] TextArea \"Note\".",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "line 3") || stats.rows != 1 {
		t.Errorf("the lines were listed, or miscounted (%d):\n%s", stats.rows, text)
	}

	// A change elsewhere is still its own line.
	elsewhere := noteTree(70, "new", 1000)
	elsewhere.Elements[2].Title = "Renamed"
	text, stats, why, ok = diffAnswer(before, elsewhere, rowsOf(before), rowsOf(elsewhere), textArea)
	if !ok || stats.rows != 2 || !strings.Contains(text, `~ [3] Button "Renamed"`) {
		t.Fatalf("a change outside the element: %v %q\n%s", ok, why, text)
	}
}

func TestDiffSaysTheActedElementWhenOnlyWhatIsUnderItChanged(t *testing.T) {
	before := noteTree(60, "same", 0)
	after := noteTree(60, "same", 1000)
	text, stats, why, ok := diffAnswer(before, after, rowsOf(before), rowsOf(after), before.id(6))
	if !ok || stats.rows != 1 {
		t.Fatalf("%v %q\n%s", ok, why, text)
	}
	if !strings.Contains(text, `~ [7] TextArea "Note" = "same" (focused) — 120 rows inside it changed with it`) {
		t.Errorf("the element itself should say how it reads:\n%s", text)
	}
}

func TestDiffStillFallsBackWithAnActedElement(t *testing.T) {
	before := noteTree(60, "old", 0)
	acted := before.id(6)

	window := noteTree(60, "new", 1000)
	window.Elements = append(window.Elements, Element{Role: "AXWindow", Title: "Second", Enabled: true})
	window.IDs = append(window.IDs, 5000)
	if _, _, why, ok := diffAnswer(before, window, rowsOf(before), rowsOf(window), acted); ok || !strings.Contains(why, "window") {
		t.Errorf("a new window: %v %q", ok, why)
	}

	cut := noteTree(60, "new", 1000)
	cut.Truncated = true
	if _, _, why, ok := diffAnswer(before, cut, rowsOf(before), rowsOf(cut), acted); ok || !strings.Contains(why, "more than was read") {
		t.Errorf("a cut read: %v %q", ok, why)
	}

	// An element that is gone from the new read can't be shown as it reads,
	// so nothing under it is folded away.
	gone := noteTree(0, "new", 1000)
	gone.Elements = gone.Elements[:6]
	gone.IDs = gone.IDs[:6]
	if text, _, _, ok := diffAnswer(before, gone, rowsOf(before), rowsOf(gone), acted); ok {
		t.Errorf("sixty-one rows gone should not be one change:\n%s", text)
	}
}

func TestDiffFallbackSaysWhereTheChangesAre(t *testing.T) {
	before := noteTree(60, "old", 0)
	after := noteTree(60, "new", 1000)
	for i := 1; i <= 5; i++ {
		after.Elements[i].Title += " changed"
	}
	// Fifty of the sixty-eight rows change outside the acted element.
	other := bigTree(60)
	other.Elements = append(other.Elements, Element{Depth: 1, Role: "AXTextArea", Title: "Note", Enabled: true})
	for i := 1; i <= 45; i++ {
		other.Elements[i].Title += " changed"
	}
	_, _, why, ok := diffAnswer(bigTree(60), other, rowsOf(bigTree(60)), rowsOf(other), 0)
	if ok || !strings.Contains(why, "much of it changed: 46 rows changed") || !strings.Contains(why, "Button ×45") {
		t.Errorf("elsewhere is named by role: %v %q", ok, why)
	}
	_, _, why, ok = diffAnswer(before, after, rowsOf(before), rowsOf(after), 0)
	if ok || !strings.Contains(why, "much of it changed") {
		t.Errorf("%v %q", ok, why)
	}
	// With the element named, the breakdown separates inside from elsewhere.
	many := noteTree(60, "new", 1000)
	for i := 0; i < 40; i++ {
		many.Elements = append(many.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("New %d", i), Enabled: true})
		many.IDs = append(many.IDs, 2000+i)
	}
	_, _, why, ok = diffAnswer(before, many, rowsOf(before), rowsOf(many), before.id(6))
	if ok || !strings.Contains(why, "inside [7] TextArea") || !strings.Contains(why, "41 elsewhere") || !strings.Contains(why, "Button ×40") {
		t.Errorf("%v %q", ok, why)
	}
}

func TestDiffNeverShowsAPasswordUnderTheActedElement(t *testing.T) {
	secret := func(value string) ElementTree {
		tree := bigTree(30)
		tree.Elements = append(tree.Elements, Element{Depth: 1, Role: "AXGroup", Title: "Login", Enabled: true},
			Element{Depth: 2, Role: "AXTextField", Subrole: "AXSecureTextField", Title: "Password", Value: value, Enabled: true})
		return tree
	}
	before, after := secret("hunter2"), secret("hunter3")
	text, _, why, ok := diffAnswer(before, after, rowsOf(before), rowsOf(after), before.id(31))
	if !ok {
		t.Fatal(why)
	}
	if strings.Contains(text, "hunter") {
		t.Errorf("a password leaked into the difference:\n%s", text)
	}
}

// Setting a text area's value rebuilds the lines under it; the answer is the
// area as it reads now, not the lines.
func TestSettingATextAreaAnswersWithTheAreaNotItsLines(t *testing.T) {
	s, backend, _ := newAgentService(t)
	app := FakeApp{App: App{Name: "Notes", PID: 505, Active: true}}
	app.Elements = append(app.Elements, Element{Role: "AXWindow", Title: "Notes", Enabled: true})
	for i := 0; i < 12; i++ {
		app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("Folder %d", i), Enabled: true, Actions: []string{"AXPress"}})
	}
	app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXTextArea", Title: "Note", Value: "old", Enabled: true, Actions: []string{"AXConfirm"}})
	for i := 0; i < 40; i++ {
		app.Elements = append(app.Elements, Element{Depth: 2, Role: "AXStaticText", Value: fmt.Sprintf("old line %d", i), Enabled: true})
	}
	backend.RunningApps = []FakeApp{app}
	backend.OnElementAction = func(app *FakeApp, index int, action string) {
		if app.Elements[index].Role != "AXTextArea" {
			return
		}
		app.Elements = app.Elements[:index+1]
		for i := 0; i < 55; i++ {
			// A different role: the lines are new elements, with new ids.
			app.Elements = append(app.Elements, Element{Depth: 2, Role: "AXGroup", Title: fmt.Sprintf("new line %d", i), Enabled: true})
		}
	}
	state := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	set := act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, state, `TextArea "Note"`), ElementAction: "set_value", Text: "new"})
	if !set.diff || !strings.Contains(set.Text, `TextArea "Note" = "new"`) || !strings.Contains(set.Text, "rows inside it changed with it") || strings.Contains(set.Text, "new line 3") {
		t.Fatalf("set_value:\n%s", set.Text)
	}
	if len(set.Text)*3 > len(state) {
		t.Errorf("the answer (%d bytes) isn't much smaller than the read (%d)", len(set.Text), len(state))
	}
}

// A pop-up button's menu is a child of the button. What a press opens is what
// the agent picks from, so it is listed row by row, never folded into the
// button.
func TestAPressedPopUpButtonsMenuItemsAreListedWithTheirIds(t *testing.T) {
	s, backend, _ := newAgentService(t)
	app := FakeApp{App: App{Name: "Prefs", PID: 606, Active: true}}
	app.Elements = append(app.Elements, Element{Role: "AXWindow", Title: "Prefs", Enabled: true})
	for i := 0; i < 20; i++ {
		app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXCheckBox", Title: fmt.Sprintf("Option %d", i), Enabled: true, Actions: []string{"AXPress"}})
	}
	app.Elements = append(app.Elements, Element{Depth: 1, Role: "AXPopUpButton", Title: "Language", Value: "English", Enabled: true, Actions: []string{"AXPress"}})
	backend.RunningApps = []FakeApp{app}
	backend.OnElementAction = func(app *FakeApp, index int, action string) {
		if app.Elements[index].Role != "AXPopUpButton" || action != "AXPress" {
			return
		}
		app.Elements = append(app.Elements[:index+1], Element{Depth: 2, Role: "AXMenu", Enabled: true},
			Element{Depth: 3, Role: "AXMenuItem", Title: "English", Enabled: true, Actions: []string{"AXPress"}},
			Element{Depth: 3, Role: "AXMenuItem", Title: "Deutsch", Enabled: true, Actions: []string{"AXPress"}},
			Element{Depth: 3, Role: "AXMenuItem", Title: "Français", Enabled: true, Actions: []string{"AXPress"}})
	}
	state := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	pressed := act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, state, `PopUpButton "Language"`), ElementAction: "press"})
	if !pressed.diff {
		t.Fatalf("a menu opening is a small difference:\n%s", pressed.Text)
	}
	for _, want := range []string{`+ [`, `Menu`, `MenuItem "English"`, `MenuItem "Deutsch"`, `MenuItem "Français"`} {
		if !strings.Contains(pressed.Text, want) {
			t.Errorf("missing %q:\n%s", want, pressed.Text)
		}
	}
	if strings.Contains(pressed.Text, "inside it changed") {
		t.Errorf("the menu was folded into the button:\n%s", pressed.Text)
	}
	// And an item is acted on by the id it was given.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Match: &ElementMatch{Role: "MenuItem", Title: "Deutsch"}, ElementAction: "press"}); err != nil {
		t.Fatalf("pick a menu item: %v", err)
	}
}

// A small change under a text area is still listed row by row: folding is for
// a difference that is too large to give.
func TestSmallChangesUnderASetValueElementAreListed(t *testing.T) {
	smallBefore := noteTree(2, "old", 0)
	smallAfter := noteTree(2, "new", 1000)
	for i := 0; i < 30; i++ {
		smallBefore.Elements = append(smallBefore.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("B%d", i), Enabled: true})
		smallAfter.Elements = append(smallAfter.Elements, Element{Depth: 1, Role: "AXButton", Title: fmt.Sprintf("B%d", i), Enabled: true})
		smallBefore.IDs = append(smallBefore.IDs, 3000+i)
		smallAfter.IDs = append(smallAfter.IDs, 3000+i)
	}
	text, stats, why, ok := diffAnswer(smallBefore, smallAfter, rowsOf(smallBefore), rowsOf(smallAfter), smallBefore.id(6))
	if !ok || stats.rows != 5 || strings.Contains(text, "inside it changed") || !strings.Contains(text, "old line 0") && !strings.Contains(text, "new line 0") {
		t.Fatalf("a small difference is listed row by row: %v %q rows %d\n%s", ok, why, stats.rows, text)
	}
}
