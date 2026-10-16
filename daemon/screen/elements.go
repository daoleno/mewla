package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"unicode/utf8"
)

// ErrNoElements means this computer can't read what is on its screen as
// elements; screenshots still work.
var ErrNoElements = errors.New("Reading the screen's elements works on Macs; use screenshots here")

// Element is one accessibility element of the focused window, as a backend
// reads it, in framebuffer pixels. Depth is its nesting under the window.
type Element struct {
	Depth   int
	Role    string
	Title   string
	Value   string
	Frame   image.Rectangle
	Enabled bool
	Focused bool
	// Actions are the element's own action names, when the backend reads
	// them.
	Actions []string
	// ref is the backend's handle to the live element, valid until the
	// tree is released.
	ref uintptr
}

// ElementTree is the focused app's focused window, flattened in reading
// order.
type ElementTree struct {
	App      string
	Window   string
	Elements []Element
	// Truncated means the backend stopped early: too many elements, or the
	// app was slow to answer.
	Truncated bool
	// PID is the app's process, when read by app.
	PID int
	// IDs are the elements' ids, when an agent's app read gave them out;
	// otherwise element i's id is i+1.
	IDs []int
}

// id is element i's id.
func (t *ElementTree) id(i int) int {
	if i < len(t.IDs) {
		return t.IDs[i]
	}
	return i + 1
}

// index is the element with id, or -1.
func (t *ElementTree) index(id int) int {
	for i := range t.Elements {
		if t.id(i) == id {
			return i
		}
	}
	return -1
}

// elementReader is a backend that can read the focused window's elements.
type elementReader interface {
	Elements(ctx context.Context, limit int) (ElementTree, error)
}

const (
	elementsRead  = 1500 // elements a backend reads at most
	elementsShown = 300  // lines an agent gets at most
	elementText   = 80   // characters of a title or value
)

// Elements without a name or value are shown only when they do something.
var actionableRoles = map[string]bool{
	"AXButton": true, "AXCheckBox": true, "AXRadioButton": true, "AXTextField": true,
	"AXTextArea": true, "AXSearchField": true, "AXComboBox": true, "AXPopUpButton": true,
	"AXMenuButton": true, "AXMenuItem": true, "AXLink": true, "AXSlider": true,
	"AXTab": true, "AXDisclosureTriangle": true, "AXIncrementor": true, "AXSecureTextField": true,
}

// formatElements lists the elements an agent can act on, with the center
// of each in screenshot pixels, ready for a click. Elements off the screen
// and empty containers are left out. Secure text fields never show a value.
func formatElements(tree ElementTree, screen image.Rectangle, toAgent func(image.Point) (int, int)) string {
	var b strings.Builder
	header := "Focused app: " + oneLine(tree.App)
	if tree.Window != "" {
		header += `, window "` + oneLine(tree.Window) + `"`
	}
	b.WriteString(header + "\nEach line: role \"name\" = \"value\" at (x, y) size w×h, in screenshot pixels; click the point to use it.\n")
	shown, hidden := 0, 0
	for _, e := range tree.Elements {
		if e.Frame.Empty() || !e.Frame.Overlaps(screen) {
			continue
		}
		title, value := oneLine(e.Title), oneLine(e.Value)
		if e.Role == "AXSecureTextField" {
			value = ""
		}
		if title == "" && value == "" && !actionableRoles[e.Role] {
			continue
		}
		if shown == elementsShown {
			hidden++
			continue
		}
		shown++
		frame := e.Frame.Intersect(screen)
		center := image.Pt((frame.Min.X+frame.Max.X)/2, (frame.Min.Y+frame.Max.Y)/2)
		x, y := toAgent(center)
		x0, y0 := toAgent(frame.Min)
		x1, y1 := toAgent(frame.Max)
		b.WriteString(strings.Repeat("  ", min(e.Depth, 12)))
		b.WriteString(strings.ToLower(strings.TrimPrefix(e.Role, "AX")))
		if title != "" {
			b.WriteString(` "` + title + `"`)
		}
		if value != "" {
			b.WriteString(` = "` + value + `"`)
		}
		fmt.Fprintf(&b, " at (%d, %d) size %d×%d", x, y, x1-x0, y1-y0)
		if !e.Enabled {
			b.WriteString(", disabled")
		}
		if e.Focused {
			b.WriteString(", focused")
		}
		b.WriteByte('\n')
	}
	switch {
	case shown == 0:
		b.WriteString("No elements to show; this app exposes little to accessibility. Use a screenshot.\n")
	case hidden > 0 || tree.Truncated:
		b.WriteString("More elements were left out; take a screenshot to see the rest.\n")
	}
	return b.String()
}

// oneLine keeps a title or value short and on one line.
func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	text = strings.ReplaceAll(text, `"`, `'`)
	if utf8.RuneCountInString(text) <= elementText {
		return text
	}
	runes := []rune(text)
	return string(runes[:elementText-1]) + "…"
}
