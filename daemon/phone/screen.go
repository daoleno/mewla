package phone

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

const (
	// MaxElements is how many elements one reading shows the agent.
	MaxElements = 300
	// MaxLabelRunes cuts the text, description and hint of one element.
	MaxLabelRunes = 120
	// maxKnownKeys bounds what a connection remembers of elements no longer on the screen.
	maxKnownKeys = 5000
	// windowTypeInputMethod is AccessibilityWindowInfo.TYPE_INPUT_METHOD: the keyboard.
	windowTypeInputMethod = 2
	windowTypeApplication = 1
	interactiveMask       = FlagClickable | FlagLongClickable | FlagEditable | FlagScrollable | FlagCheckable
)

// Element is one thing on the agent's display that an agent can name by its ID.
type Element struct {
	ID    int
	Role  string
	Text  string
	Desc  string
	Hint  string
	Res   string
	Flags int
	// Window is the index of the element's window in the reading, top first.
	Window int
	// Bounds are the element's bounds clipped to the display; X, Y is their center.
	Bounds [4]int
	X, Y   int
}

// Label is the element's text, else its description, else its hint.
func (e Element) Label() string {
	switch {
	case e.Text != "":
		return e.Text
	case e.Desc != "":
		return e.Desc
	}
	return e.Hint
}

// WindowInfo is a window that has elements in a reading.
type WindowInfo struct {
	Pkg   string
	Title string
}

// View is one reading of the agent's display, ready to show an agent.
type View struct {
	W, H int
	// Top is the app in front.
	Top      WindowInfo
	Keyboard bool
	Windows  []WindowInfo
	Elements []Element
	// Omitted counts elements beyond MaxElements, which have no id.
	Omitted int
}

// Elements gives the elements of one phone connection their ids. An element keeps its id for as
// long as it stays the same element (same app, view id, class, text and description, and the
// same place among identical ones), including while it scrolls out of sight and back. Ids are
// never reused, and an id whose element is not on the latest reading is refused, never moved to
// another element.
type Elements struct {
	mu   sync.Mutex
	next int
	ids  map[string]int
	live map[int]Element
	w, h int
}

func NewElements() *Elements {
	return &Elements{ids: map[string]int{}, live: map[int]Element{}}
}

// Size is the display's size in the latest reading, 0 x 0 before one.
func (e *Elements) Size() (w, h int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.w, e.h
}

type candidate struct {
	el    Element
	key   string
	label string
}

// Observe takes a reading of the display: it gives its elements ids and replaces what Resolve
// knows with it.
func (e *Elements) Observe(s *Screen) View {
	view := View{}
	if s == nil {
		e.mu.Lock()
		e.live = map[int]Element{}
		e.mu.Unlock()
		return view
	}
	view.W, view.H = s.W, s.H
	var cands []candidate
	// The app in front is the first application window the system calls active or focused, else
	// the first application window, else the first window; elements or not.
	var activeApp, firstApp, firstAny *WindowInfo
	for i := range s.Windows {
		w := s.Windows[i]
		if w.Type == windowTypeInputMethod {
			// Its keys are for the person's fingers; the agent types with the text op.
			view.Keyboard = true
			continue
		}
		info := &WindowInfo{Pkg: w.Pkg, Title: w.Title}
		if firstAny == nil {
			firstAny = info
		}
		if w.Type == windowTypeApplication {
			if firstApp == nil {
				firstApp = info
			}
			if activeApp == nil && (w.Active || w.Focused) {
				activeApp = info
			}
		}
		before := len(cands)
		cands = appendWindow(cands, w, len(view.Windows), s.W, s.H)
		if len(cands) > before {
			view.Windows = append(view.Windows, *info)
		}
	}
	for _, top := range []*WindowInfo{activeApp, firstApp, firstAny} {
		if top != nil {
			view.Top = *top
			break
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.w, e.h = s.W, s.H
	live := make(map[int]Element, len(cands))
	seen := map[string]int{}
	for _, c := range cands {
		n := seen[c.key]
		seen[c.key] = n + 1
		full := c.key + "#" + strconv.Itoa(n)
		id, known := e.ids[full]
		if !known {
			if len(view.Elements) >= MaxElements {
				view.Omitted++
				continue
			}
			e.next++
			id = e.next
			e.ids[full] = id
		} else if len(view.Elements) >= MaxElements {
			view.Omitted++
			continue
		}
		el := c.el
		el.ID = id
		live[id] = el
		view.Elements = append(view.Elements, el)
	}
	if len(e.ids) > maxKnownKeys {
		// Forget elements that left the screen; their ids stay retired, never reused.
		kept := make(map[string]int, len(live))
		for full, id := range e.ids {
			if _, ok := live[id]; ok {
				kept[full] = id
			}
		}
		e.ids = kept
	}
	e.live = live
	return view
}

// appendWindow adds a window's elements in tree order.
func appendWindow(cands []candidate, w Window, window, dw, dh int) []candidate {
	absorbed := map[int]bool{}
	for i, n := range w.Nodes {
		if absorbed[i] {
			continue
		}
		interactive := n.F&interactiveMask != 0
		text, desc, hint := cleanLabel(n.T), cleanLabel(n.X), cleanLabel(n.H)
		if text == "" && desc == "" && hint == "" && !interactive {
			continue
		}
		bounds, ok := clip(n.B, dw, dh)
		if !ok {
			continue
		}
		if n.F&(FlagClickable|FlagLongClickable|FlagCheckable) != 0 && text == "" && desc == "" && hint == "" {
			// A pressable row with nothing to say itself says what its text children do. A
			// scrolling container does not: its rows say their own.
			text = absorbChildren(w.Nodes, i, absorbed)
		}
		el := Element{
			Role: roleOf(n.C, n.F), Text: text, Desc: desc, Hint: hint, Res: n.R, Flags: n.F, Window: window,
			Bounds: bounds, X: (bounds[0] + bounds[2]) / 2, Y: (bounds[1] + bounds[3]) / 2,
		}
		// A field is the same field as its text changes under the agent's typing.
		keyText := text
		if n.F&FlagEditable != 0 {
			keyText = hint
		}
		key := strings.Join([]string{w.Pkg, n.R, n.C, keyText, desc}, "|")
		cands = append(cands, candidate{el: el, key: key})
	}
	return cands
}

// absorbChildren is the text of up to three plain text nodes below nodes[i], which are then not
// elements of their own. A pressable node below is its own element, and so is everything in it.
func absorbChildren(nodes []Node, i int, absorbed map[int]bool) string {
	var parts []string
	for j := i + 1; j < len(nodes) && nodes[j].D > nodes[i].D; j++ {
		c := nodes[j]
		if c.F&interactiveMask != 0 {
			for j+1 < len(nodes) && nodes[j+1].D > c.D {
				j++
			}
			continue
		}
		if absorbed[j] {
			continue
		}
		label := cleanLabel(c.T)
		if label == "" {
			label = cleanLabel(c.X)
		}
		if label == "" {
			continue
		}
		parts = append(parts, label)
		absorbed[j] = true
		if len(parts) == 3 {
			break
		}
	}
	return cut(strings.Join(parts, " · "))
}

// clip restricts bounds to the display; ok is false when nothing of them is on it.
func clip(b [4]int, w, h int) ([4]int, bool) {
	if w > 0 && h > 0 {
		b[0], b[1] = max(b[0], 0), max(b[1], 0)
		b[2], b[3] = min(b[2], w), min(b[3], h)
	}
	return b, b[2] > b[0] && b[3] > b[1]
}

// cleanLabel is a node's text on one line.
func cleanLabel(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '​' }), " ")
}

func cut(s string) string {
	if r := []rune(s); len(r) > MaxLabelRunes {
		return strings.TrimSpace(string(r[:MaxLabelRunes])) + "…"
	}
	return s
}

func roleOf(class string, flags int) string {
	c := strings.ToLower(class)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(c, p) {
				return true
			}
		}
		return false
	}
	switch {
	case flags&FlagEditable != 0 || has("edittext", "autocompletetextview", "textfield"):
		return "field"
	case has("imagebutton"):
		return "imagebutton"
	case has("radiobutton"):
		return "radio"
	case has("checkbox"):
		return "checkbox"
	case has("switch", "togglebutton"):
		return "switch"
	case has("button"):
		return "button"
	case has("imageview"):
		return "image"
	case has("seekbar", "slider"):
		return "slider"
	case has("progressbar"):
		return "progress"
	case has("spinner"):
		return "dropdown"
	case has("webview"):
		return "web"
	case has("recyclerview", "listview", "gridview", "scrollview", "viewpager", "lazycolumn", "lazyrow"):
		return "list"
	case has("tab"):
		return "tab"
	case has("textview"):
		return "text"
	case flags&FlagCheckable != 0:
		return "checkbox"
	}
	return "view"
}

// flagWords are the short words an element's flags are shown as.
func flagWords(e Element) []string {
	var words []string
	f := e.Flags
	if f&FlagClickable != 0 && f&FlagEditable == 0 {
		words = append(words, "tap")
	}
	if f&FlagLongClickable != 0 {
		words = append(words, "long")
	}
	if f&FlagEditable != 0 {
		words = append(words, "edit")
	}
	if f&FlagScrollable != 0 {
		words = append(words, "scroll")
	}
	if f&FlagCheckable != 0 {
		if f&FlagChecked != 0 {
			words = append(words, "checked")
		} else {
			words = append(words, "off")
		}
	}
	if f&FlagFocused != 0 {
		words = append(words, "focused")
	}
	if f&FlagSelected != 0 {
		words = append(words, "selected")
	}
	if f&FlagEnabled == 0 {
		words = append(words, "disabled")
	}
	if f&FlagPassword != 0 {
		words = append(words, "password")
	}
	return words
}

func quoted(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(cut(s), `\`, `\\`), `"`, `\"`) + `"`
}

// Render is the reading as the text an agent sees: a header line, then one line per element.
func (v View) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Screen %dx%d", v.W, v.H)
	if v.Top.Pkg != "" || v.Top.Title != "" {
		b.WriteString(", app " + v.Top.Pkg)
		if v.Top.Title != "" {
			b.WriteString(" " + quoted(v.Top.Title))
		}
	}
	if v.Keyboard {
		b.WriteString(", keyboard open")
	}
	if v.Omitted > 0 {
		fmt.Fprintf(&b, ", %d of %d elements shown", len(v.Elements), len(v.Elements)+v.Omitted)
	} else {
		fmt.Fprintf(&b, ", %d elements", len(v.Elements))
	}
	b.WriteByte('\n')
	lastWindow := -1
	for _, e := range v.Elements {
		if len(v.Windows) > 1 && e.Window != lastWindow {
			w := v.Windows[e.Window]
			fmt.Fprintf(&b, "-- window %s", w.Pkg)
			if w.Title != "" {
				b.WriteString(" " + quoted(w.Title))
			}
			b.WriteByte('\n')
			lastWindow = e.Window
		}
		fmt.Fprintf(&b, "[%d] %s", e.ID, e.Role)
		if e.Text != "" {
			b.WriteString(" " + quoted(e.Text))
		}
		if e.Desc != "" {
			b.WriteString(" desc=" + quoted(e.Desc))
		}
		if e.Hint != "" {
			b.WriteString(" hint=" + quoted(e.Hint))
		}
		if e.Text == "" && e.Desc == "" && e.Hint == "" && e.Res != "" {
			b.WriteString(" res=" + e.Res)
		}
		fmt.Fprintf(&b, " (%d,%d)", e.X, e.Y)
		if words := flagWords(e); len(words) > 0 {
			b.WriteString(" " + strings.Join(words, " "))
		}
		b.WriteByte('\n')
	}
	if len(v.Elements) == 0 {
		b.WriteString("(nothing readable here: the app may draw its own screen; use a picture)\n")
	}
	if v.Omitted > 0 {
		fmt.Fprintf(&b, "… %d more elements not shown; act on what is shown or scroll, then read again\n", v.Omitted)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Find is the first element whose text, description or hint contains match, ignoring case.
func (v View) Find(match string) (Element, bool) {
	match = strings.ToLower(strings.TrimSpace(match))
	if match == "" {
		return Element{}, false
	}
	for _, e := range v.Elements {
		for _, s := range []string{e.Text, e.Desc, e.Hint} {
			if strings.Contains(strings.ToLower(s), match) {
				return e, true
			}
		}
	}
	return Element{}, false
}

// Resolve is the element an id names on the latest reading. An id that is not on it is refused:
// the agent reads the screen again.
func (e *Elements) Resolve(id int) (Element, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if el, ok := e.live[id]; ok {
		return el, nil
	}
	if id <= 0 || id > e.next {
		return Element{}, fmt.Errorf("there is no element [%d]: read the screen and use an id from it", id)
	}
	return Element{}, fmt.Errorf("element [%d] is not on the screen any more: read the screen again and use an id from it", id)
}
