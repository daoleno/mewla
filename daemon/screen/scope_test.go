package screen

import (
	"image"
	"strings"
	"testing"
)

func scopeTree() ElementTree {
	return ElementTree{App: "X", Elements: []Element{
		{Depth: 0, Role: "AXWindow", Title: "Main", Enabled: true},
		{Depth: 1, Role: "AXGroup", Title: "Sidebar", Enabled: true},
		{Depth: 2, Role: "AXButton", Title: "General", Enabled: true},
		{Depth: 2, Role: "AXButton", Title: "About", Enabled: true},
		{Depth: 3, Role: "AXStaticText", Title: "deep", Enabled: true},
		{Depth: 1, Role: "AXGroup", Title: "Content", Enabled: true},
		{Depth: 2, Role: "AXButton", Title: "About this Mac", Enabled: true},
	}}
}

func scoped(t *testing.T, s appScope) string {
	t.Helper()
	text, _, err := formatAppTree(App{Name: "X", PID: 1}, scopeTree(), image.Rect(0, 0, 100, 100), func(p image.Point) (int, int) { return p.X, p.Y }, s)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestScopeMatchListsOnlyMatches(t *testing.T) {
	text := scoped(t, appScope{Match: &ElementMatch{Role: "Button", Title: "About"}})
	if !strings.Contains(text, `[4] Button "About"`) || strings.Contains(text, "General") || strings.Contains(text, "this Mac") {
		t.Fatalf("whole-title match only:\n%s", text)
	}
	if !strings.Contains(text, "1 found") {
		t.Fatal(text)
	}
}

func TestScopeUnderAndDepth(t *testing.T) {
	text := scoped(t, appScope{Under: 2})
	if !strings.Contains(text, "General") || !strings.Contains(text, "deep") || strings.Contains(text, "Content") || strings.Contains(text, "[1] Window") {
		t.Fatalf("inside [2]:\n%s", text)
	}
	text = scoped(t, appScope{Under: 2, Depth: 1})
	if !strings.Contains(text, "General") || strings.Contains(text, "deep") {
		t.Fatalf("one level:\n%s", text)
	}
	if text := scoped(t, appScope{Depth: 1}); strings.Contains(text, "General") || !strings.Contains(text, "Sidebar") {
		t.Fatalf("skeleton:\n%s", text)
	}
}

func TestScopeUnknownIDAndNothing(t *testing.T) {
	if _, _, err := formatAppTree(App{Name: "X"}, scopeTree(), image.Rect(0, 0, 9, 9), func(p image.Point) (int, int) { return 0, 0 }, appScope{Under: 99}); err == nil {
		t.Fatal("a gone id must be refused")
	}
	if text := scoped(t, appScope{Match: &ElementMatch{Role: "Slider"}}); !strings.Contains(text, "0 found") || !strings.Contains(text, "Nothing in that scope") {
		t.Fatal(text)
	}
}
