package phone

import (
	"fmt"
	"strings"
	"testing"
)

const (
	tap1   = FlagClickable | FlagEnabled
	plain  = FlagEnabled
	field  = FlagEditable | FlagClickable | FlagEnabled
	scroll = FlagScrollable | FlagEnabled
)

func node(class, res, text, desc string, flags int, b [4]int) Node {
	return Node{C: class, R: res, T: text, X: desc, B: b, F: flags}
}

func appScreen(nodes ...Node) *Screen {
	return &Screen{W: 1080, H: 2400, Windows: []Window{{ID: 1, Type: 1, Active: true, Focused: true, Pkg: "com.example", Title: "Example", Bounds: [4]int{0, 0, 1080, 2400}, Nodes: nodes}}}
}

// row is a list item with a distinct title.
func row(i, top int) Node {
	return node("TextView", "title", fmt.Sprintf("Item %d", i), "", tap1, [4]int{0, top, 1080, top + 200})
}

func idOf(t *testing.T, v View, text string) int {
	t.Helper()
	for _, e := range v.Elements {
		if e.Text == text || e.Desc == text {
			return e.ID
		}
	}
	t.Fatalf("no element %q in\n%s", text, v.Render())
	return 0
}

func TestElementsKeepTheirIdsWhileTheyRemain(t *testing.T) {
	ids := NewElements()
	first := ids.Observe(appScreen(
		node("Button", "back", "", "Navigate up", tap1, [4]int{0, 0, 100, 100}),
		row(1, 200), row(2, 400), row(3, 600), row(4, 800),
	))
	if got := idOf(t, first, "Navigate up"); got != 1 {
		t.Fatalf("ids start at 1, got %d", got)
	}
	i2, i3, i4 := idOf(t, first, "Item 2"), idOf(t, first, "Item 3"), idOf(t, first, "Item 4")

	// The list scrolls by two rows: items 3 and 4 remain, 1 and 2 left, 5 and 6 arrived.
	second := ids.Observe(appScreen(
		node("Button", "back", "", "Navigate up", tap1, [4]int{0, 0, 100, 100}),
		row(3, 200), row(4, 400), row(5, 600), row(6, 800),
	))
	if idOf(t, second, "Item 3") != i3 || idOf(t, second, "Item 4") != i4 || idOf(t, second, "Navigate up") != 1 {
		t.Fatalf("ids moved when the list scrolled:\n%s\n%s", first.Render(), second.Render())
	}
	i5 := idOf(t, second, "Item 5")
	if i5 <= i4 {
		t.Fatalf("a new element got an old id: %d", i5)
	}
	// Item 3 now sits at a new place: asking for it taps where it is now, not where it was.
	if el, err := ids.Resolve(i3); err != nil || el.Y != 300 || el.X != 540 {
		t.Fatalf("item 3 resolves to %+v %v", el, err)
	}

	// Items that scrolled out are refused, never given to another element.
	if _, err := ids.Resolve(i2); err == nil || !strings.Contains(err.Error(), "read the screen again") {
		t.Fatalf("a gone element resolved: %v", err)
	}
	if _, err := ids.Resolve(9999); err == nil {
		t.Fatal("an id never given out resolved")
	}
	if _, err := ids.Resolve(0); err == nil {
		t.Fatal("id 0 resolved")
	}

	// Scrolling back brings the same elements back under the same ids; none is reused.
	third := ids.Observe(appScreen(row(1, 200), row(2, 400), row(3, 600), row(4, 800)))
	if idOf(t, third, "Item 2") != i2 || idOf(t, third, "Item 3") != i3 {
		t.Fatalf("returning elements got new ids:\n%s", third.Render())
	}
	seen := map[int]string{}
	for _, v := range []View{first, second, third} {
		for _, e := range v.Elements {
			if prev, ok := seen[e.ID]; ok && prev != e.Label() {
				t.Fatalf("id %d named %q and %q", e.ID, prev, e.Label())
			}
			seen[e.ID] = e.Label()
		}
	}
}

func TestIdenticalElementsAreToldApartByTheirPlaceAmongThem(t *testing.T) {
	// Known limit: identical elements are told apart only by their order, so when one of
	// them scrolls out the ones after it take the ids of the ones before. Ids stay unique and
	// the answer to every action shows the screen, but an id can move between identical rows.
	ids := NewElements()
	del := func(top int) Node {
		return node("Button", "delete", "Delete", "", tap1, [4]int{0, top, 200, top + 100})
	}
	first := ids.Observe(appScreen(del(100), del(300), del(500)))
	if len(first.Elements) != 3 || first.Elements[0].ID == first.Elements[1].ID || first.Elements[1].ID == first.Elements[2].ID {
		t.Fatalf("identical elements share ids:\n%s", first.Render())
	}
	again := ids.Observe(appScreen(del(100), del(300), del(500)))
	for i := range again.Elements {
		if again.Elements[i].ID != first.Elements[i].ID {
			t.Fatalf("identical elements changed ids with nothing changed")
		}
	}
	scrolled := ids.Observe(appScreen(del(100), del(300)))
	if scrolled.Elements[0].ID != first.Elements[0].ID || scrolled.Elements[1].ID != first.Elements[1].ID {
		t.Fatalf("pins the current behavior: the first two keep their ids by order")
	}
	if _, err := ids.Resolve(first.Elements[2].ID); err == nil {
		t.Fatal("the third Delete is gone and must be refused")
	}
}

func TestAChangedElementIsANewElement(t *testing.T) {
	ids := NewElements()
	a := ids.Observe(appScreen(node("TextView", "clock", "10:41", "", plain, [4]int{0, 0, 100, 50})))
	b := ids.Observe(appScreen(node("TextView", "clock", "10:42", "", plain, [4]int{0, 0, 100, 50})))
	if a.Elements[0].ID == b.Elements[0].ID {
		t.Fatal("an element with other text kept its id")
	}
	if _, err := ids.Resolve(a.Elements[0].ID); err == nil {
		t.Fatal("the old clock resolved")
	}
}

func TestAFieldIsTheSameFieldWhileTheAgentTypes(t *testing.T) {
	ids := NewElements()
	empty := node("EditText", "query", "", "", field, [4]int{0, 100, 1000, 200})
	empty.H = "Search"
	typed := empty
	typed.T = "hello"
	a := ids.Observe(appScreen(empty))
	b := ids.Observe(appScreen(typed))
	if a.Elements[0].ID != b.Elements[0].ID {
		t.Fatalf("the field changed ids as its text did: %d -> %d", a.Elements[0].ID, b.Elements[0].ID)
	}
}

func TestOnlyWhatIsWorthNamingBecomesAnElement(t *testing.T) {
	ids := NewElements()
	s := appScreen(
		node("View", "", "", "", plain, [4]int{0, 0, 1080, 2400}),        // nothing to say or press
		node("TextView", "", "Visible", "", plain, [4]int{0, 0, 10, 10}), // text
		node("TextView", "", "Off screen", "", plain, [4]int{0, 3000, 10, 3100}),
		node("TextView", "", "Zero size", "", plain, [4]int{5, 5, 5, 5}),
		node("View", "", "", "", tap1, [4]int{0, 100, 50, 150}), // pressable, unnamed
	)
	v := ids.Observe(s)
	if len(v.Elements) != 2 || v.Elements[0].Text != "Visible" {
		t.Fatalf("elements:\n%s", v.Render())
	}
	// A partly visible element is pressed in its visible part.
	v = ids.Observe(appScreen(node("Button", "", "Half", "", tap1, [4]int{900, 2300, 1300, 2700})))
	if e := v.Elements[0]; e.X != 990 || e.Y != 2350 {
		t.Fatalf("center of the visible part: %+v", e)
	}
}

func TestAPressableRowSpeaksForItsTextChildren(t *testing.T) {
	ids := NewElements()
	v := ids.Observe(appScreen(
		Node{D: 1, C: "LinearLayout", F: tap1, B: [4]int{0, 100, 1080, 300}},
		Node{D: 2, C: "TextView", T: "Wi-Fi", F: plain, B: [4]int{10, 110, 200, 150}},
		Node{D: 2, C: "TextView", T: "Connected", F: plain, B: [4]int{10, 150, 200, 200}},
		Node{D: 1, C: "TextView", T: "Not part of the row", F: plain, B: [4]int{0, 400, 500, 450}},
	))
	if len(v.Elements) != 2 || v.Elements[0].Text != "Wi-Fi · Connected" || v.Elements[1].Text != "Not part of the row" {
		t.Fatalf("elements:\n%s", v.Render())
	}
}

func TestRenderIsCompactAndSaysWhatItLeftOut(t *testing.T) {
	ids := NewElements()
	long := strings.Repeat("x", 300)
	pw := node("EditText", "pw", "", "", field|FlagPassword|FlagFocused, [4]int{0, 600, 1000, 700})
	pw.H = "Password"
	v := ids.Observe(appScreen(
		node("Button", "", "Sign in", "", tap1, [4]int{100, 1000, 300, 1100}),
		node("EditText", "email", "me@example.com", "", field, [4]int{0, 400, 1000, 500}),
		pw,
		node("Switch", "", "Wi-Fi", "", tap1|FlagCheckable|FlagChecked, [4]int{0, 800, 1000, 900}),
		node("CheckBox", "", "Remember", "", tap1|FlagCheckable, [4]int{0, 900, 1000, 1000}),
		node("ImageButton", "more", "", "More options", tap1|FlagLongClickable, [4]int{1000, 0, 1080, 100}),
		node("ImageButton", "send_button", "", "", tap1, [4]int{1000, 100, 1080, 200}),
		node("RecyclerView", "feed", "", "", scroll, [4]int{0, 1100, 1080, 2300}),
		node("TextView", "", long, "", plain, [4]int{0, 2300, 1080, 2400}),
		node("TextView", "", "Line one\nline \"two\"", "", FlagEnabled|FlagSelected, [4]int{0, 2300, 1080, 2400}),
		node("Button", "", "Greyed", "", FlagClickable, [4]int{0, 0, 10, 10}),
	))
	got := v.Render()
	want := []string{
		`Screen 1080x2400, app com.example "Example", 11 elements`,
		`[1] button "Sign in" (200,1050) tap`,
		`[2] field "me@example.com" (500,450) edit`,
		`[3] field hint="Password" (500,650) edit focused password`,
		`[4] switch "Wi-Fi" (500,850) tap checked`,
		`[5] checkbox "Remember" (500,950) tap off`,
		`[6] imagebutton desc="More options" (1040,50) tap long`,
		`[7] imagebutton res=send_button (1040,150) tap`,
		`[8] list res=feed (540,1700) scroll`,
		`[10] text "Line one line \"two\"" (540,2350) selected`,
		`[11] button "Greyed" (5,5) tap disabled`,
	}
	// The long text is cut, the rest of the lines are as written.
	if !strings.Contains(got, `[9] text "`+strings.Repeat("x", MaxLabelRunes)+`…" (540,2350)`) {
		t.Fatalf("long text not cut:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 12 {
		t.Fatalf("%d lines:\n%s", len(lines), got)
	}
	for _, w := range want {
		found := false
		for _, l := range lines {
			if l == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing line %q in\n%s", w, got)
		}
	}
	// The email typed into a field is shown (it is on the screen), a password never is.
	if strings.Contains(got, "secret") {
		t.Fatal("password text leaked")
	}
}

func TestRenderCapsTheElementsAndSaysSo(t *testing.T) {
	ids := NewElements()
	var nodes []Node
	for i := 0; i < MaxElements+25; i++ {
		nodes = append(nodes, node("TextView", "", fmt.Sprintf("Row %d", i), "", plain, [4]int{0, i, 100, i + 1}))
	}
	v := ids.Observe(appScreen(nodes...))
	if len(v.Elements) != MaxElements || v.Omitted != 25 {
		t.Fatalf("%d shown, %d omitted", len(v.Elements), v.Omitted)
	}
	out := v.Render()
	if !strings.Contains(out, "300 of 325 elements shown") || !strings.Contains(out, "25 more elements not shown") {
		t.Fatalf("truncation not announced:\n%s", out[:200])
	}
	if _, err := ids.Resolve(MaxElements + 1); err == nil {
		t.Fatal("an element that was not shown has an id")
	}
}

func TestWindowsAndTheKeyboard(t *testing.T) {
	ids := NewElements()
	s := &Screen{W: 1080, H: 2400, Windows: []Window{
		{Type: 2, Pkg: "com.keyboard", Nodes: []Node{node("Button", "", "q", "", tap1, [4]int{0, 1500, 100, 1600})}},
		{Type: 3, Pkg: "com.android.systemui", Title: "Shade", Nodes: []Node{node("TextView", "", "Alarm", "", plain, [4]int{0, 0, 100, 100})}},
		{Type: 1, Active: true, Pkg: "com.example", Title: "Mail", Nodes: []Node{node("Button", "", "Send", "", tap1, [4]int{0, 200, 100, 300})}},
	}}
	v := ids.Observe(s)
	out := v.Render()
	if !v.Keyboard || strings.Contains(out, `"q"`) {
		t.Fatalf("keyboard keys are for the person:\n%s", out)
	}
	if !strings.Contains(out, `app com.example "Mail"`) || !strings.Contains(out, "keyboard open") ||
		!strings.Contains(out, `-- window com.android.systemui "Shade"`) || !strings.Contains(out, `-- window com.example "Mail"`) {
		t.Fatalf("windows not told apart:\n%s", out)
	}
	// An app with nothing to read still has its name, and a picture is asked for.
	empty := ids.Observe(&Screen{W: 100, H: 100, Windows: []Window{{Type: 1, Active: true, Pkg: "com.game"}}})
	if empty.Top.Pkg != "com.game" || !strings.Contains(empty.Render(), "nothing readable") {
		t.Fatalf("empty screen: %s", empty.Render())
	}
}

func TestFindMatchesTextCaseInsensitively(t *testing.T) {
	ids := NewElements()
	v := ids.Observe(appScreen(node("TextView", "", "Order Shipped", "", plain, [4]int{0, 0, 10, 10}), node("ImageButton", "", "", "Search", tap1, [4]int{0, 20, 10, 30})))
	if e, ok := v.Find("shipped"); !ok || e.ID != 1 {
		t.Fatalf("find text: %+v %v", e, ok)
	}
	if e, ok := v.Find("SEARCH"); !ok || e.ID != 2 {
		t.Fatalf("find desc: %+v %v", e, ok)
	}
	if _, ok := v.Find(""); ok {
		t.Fatal("empty match found something")
	}
	if _, ok := v.Find("nothing"); ok {
		t.Fatal("found what is not there")
	}
}
