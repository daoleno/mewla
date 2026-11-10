//go:build linux && !screenfake

package screen

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// AT-SPI is how Linux apps show their buttons, fields and text to assistive
// technology, over a D-Bus bus of its own. The agent screen runs a private
// one (see vdisplay), which every app opened there joins.

const (
	atspiRegistry   = "org.a11y.atspi.Registry"
	atspiRoot       = dbus.ObjectPath("/org/a11y/atspi/accessible/root")
	atspiAccessible = "org.a11y.atspi.Accessible"
	atspiComponent  = "org.a11y.atspi.Component"
	atspiText       = "org.a11y.atspi.Text"
	atspiEditable   = "org.a11y.atspi.EditableText"
	atspiAction     = "org.a11y.atspi.Action"
	atspiValue      = "org.a11y.atspi.Value"
)

// AT-SPI states this reads (AtspiStateType).
const (
	stateDefunct   = 6
	stateEditable  = 7
	stateEnabled   = 8
	stateFocused   = 12
	stateMultiLine = 17
	stateSensitive = 24
	stateShowing   = 25
	stateVisible   = 30
)

// atspiRef is one live accessible: the app's bus name and the object's path.
type atspiRef struct {
	Bus  string
	Path dbus.ObjectPath
}

// atspi is a connection to one accessibility bus, and the live elements that
// reads handed out.
type atspi struct {
	conn *dbus.Conn

	mu   sync.Mutex
	refs map[uintptr]atspiRef
	next uintptr
}

func dialATSPI(address string) (*atspi, error) {
	if address == "" {
		return nil, ErrNoElements
	}
	conn, err := dbus.Connect(address)
	if err != nil {
		return nil, fmt.Errorf("connect to the accessibility bus: %w", err)
	}
	return &atspi{conn: conn, refs: map[uintptr]atspiRef{}}, nil
}

func (a *atspi) close() { _ = a.conn.Close() }

func (a *atspi) keep(r atspiRef) uintptr {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.next++
	a.refs[a.next] = r
	return a.next
}

func (a *atspi) ref(e Element) (atspiRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.refs[e.ref]
	return r, ok
}

func (a *atspi) release(tree ElementTree) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range tree.Elements {
		delete(a.refs, e.ref)
	}
}

// apps maps each app on the bus to its process.
func (a *atspi) apps(ctx context.Context) (map[int]atspiRef, error) {
	var children []atspiRef
	if err := a.conn.Object(atspiRegistry, atspiRoot).CallWithContext(ctx, atspiAccessible+".GetChildren", 0).Store(&children); err != nil {
		return nil, err
	}
	out := map[int]atspiRef{}
	for _, child := range children {
		var pid uint32
		if err := a.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, child.Bus).Store(&pid); err == nil && pid > 0 {
			out[int(pid)] = child
		}
	}
	return out, nil
}

// appName is the name an app gives itself, such as kwrite.
func (a *atspi) appName(ctx context.Context, app atspiRef) string {
	v, err := a.conn.Object(app.Bus, app.Path).GetProperty(atspiAccessible + ".Name")
	if err != nil {
		return ""
	}
	name, _ := v.Value().(string)
	return name
}

// node is one accessible as a read finds it.
type node struct {
	ref      atspiRef
	depth    int
	role     string
	name     string
	desc     string
	states   uint64
	ifaces   map[string]bool
	children []atspiRef
	frame    image.Rectangle
	value    string
	actions  []string
}

func (n *node) has(state uint) bool { return n.states&(1<<state) != 0 }

// read reads an app's windows, breadth first with every call of a level in
// flight at once (an app answers them in order, but the bus round trips
// overlap), then lists them in reading order. Elements not on the screen,
// such as the items of closed menus, are left out with what lies under them.
func (a *atspi) read(ctx context.Context, app atspiRef, origin image.Point, limit int) (ElementTree, error) {
	var root struct{ children []atspiRef }
	if err := a.conn.Object(app.Bus, app.Path).CallWithContext(ctx, atspiAccessible+".GetChildren", 0).Store(&root.children); err != nil {
		return ElementTree{}, err
	}
	nodes := map[atspiRef]*node{}
	level := make([]*node, 0, len(root.children))
	for _, c := range root.children {
		level = append(level, &node{ref: c})
	}
	truncated := false
	count := 0
	for len(level) > 0 {
		if count+len(level) > limit {
			level = level[:max(0, limit-count)]
			truncated = true
		}
		if err := a.fill(ctx, level); err != nil {
			if ctx.Err() != nil {
				truncated = true
				break
			}
			return ElementTree{}, err
		}
		var next []*node
		for _, n := range level {
			nodes[n.ref] = n
			count++
			if !n.has(stateShowing) || n.has(stateDefunct) {
				n.children = nil
				continue
			}
			for _, c := range n.children {
				if _, seen := nodes[c]; !seen {
					next = append(next, &node{ref: c, depth: n.depth + 1})
				}
			}
		}
		if truncated {
			break
		}
		level = next
	}
	tree := ElementTree{Truncated: truncated}
	var walk func(r atspiRef)
	walk = func(r atspiRef) {
		n := nodes[r]
		if n == nil || !n.has(stateShowing) || n.has(stateDefunct) {
			return
		}
		tree.Elements = append(tree.Elements, a.element(n, origin))
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, c := range root.children {
		walk(c)
	}
	return tree, nil
}

// fill asks for everything about one level of nodes, all in flight at once.
func (a *atspi) fill(ctx context.Context, level []*node) error {
	type pending struct {
		n                                 *node
		role, states, ifaces, props, kids *dbus.Call
	}
	calls := make([]pending, len(level))
	for i, n := range level {
		obj := a.conn.Object(n.ref.Bus, n.ref.Path)
		calls[i] = pending{n: n,
			role:   obj.GoWithContext(ctx, atspiAccessible+".GetRoleName", 0, nil),
			states: obj.GoWithContext(ctx, atspiAccessible+".GetState", 0, nil),
			ifaces: obj.GoWithContext(ctx, atspiAccessible+".GetInterfaces", 0, nil),
			props:  obj.GoWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, nil, atspiAccessible),
			kids:   obj.GoWithContext(ctx, atspiAccessible+".GetChildren", 0, nil),
		}
	}
	type second struct {
		n                   *node
		extents, text, acts *dbus.Call
		value               *dbus.Call
	}
	var more []second
	for _, p := range calls {
		n := p.n
		if err := wait(ctx, p.role).Store(&n.role); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			n.states = 1 << stateDefunct // gone while read
			continue
		}
		var states []uint32
		_ = wait(ctx, p.states).Store(&states)
		for i, word := range states {
			if i < 2 {
				n.states |= uint64(word) << (32 * i)
			}
		}
		var ifaces []string
		_ = wait(ctx, p.ifaces).Store(&ifaces)
		n.ifaces = map[string]bool{}
		for _, iface := range ifaces {
			n.ifaces[iface] = true
		}
		var props map[string]dbus.Variant
		if wait(ctx, p.props).Store(&props) == nil {
			n.name, _ = props["Name"].Value().(string)
			n.desc, _ = props["Description"].Value().(string)
		}
		_ = wait(ctx, p.kids).Store(&n.children)
		if !n.has(stateShowing) {
			continue
		}
		obj := a.conn.Object(n.ref.Bus, n.ref.Path)
		s := second{n: n}
		if n.ifaces[atspiComponent] {
			s.extents = obj.GoWithContext(ctx, atspiComponent+".GetExtents", 0, nil, uint32(0))
		}
		if n.ifaces[atspiText] && n.role != "password text" {
			s.text = obj.GoWithContext(ctx, atspiText+".GetText", 0, nil, int32(0), int32(elementValueRead))
		}
		if n.ifaces[atspiValue] {
			s.value = obj.GoWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, nil, atspiValue, "CurrentValue")
		}
		if n.ifaces[atspiAction] {
			s.acts = obj.GoWithContext(ctx, atspiAction+".GetActions", 0, nil)
		}
		more = append(more, s)
	}
	for _, s := range more {
		n := s.n
		if s.extents != nil {
			var r struct{ X, Y, W, H int32 }
			if wait(ctx, s.extents).Store(&r) == nil && r.W > 0 && r.H > 0 {
				n.frame = image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
			}
		}
		if s.text != nil {
			_ = wait(ctx, s.text).Store(&n.value)
		}
		if s.value != nil && n.value == "" {
			var v dbus.Variant
			if wait(ctx, s.value).Store(&v) == nil {
				if f, ok := v.Value().(float64); ok {
					n.value = strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.4f", f), "0000"), ".")
				}
			}
		}
		if s.acts != nil {
			var acts []struct{ Name, Desc, Key string }
			if wait(ctx, s.acts).Store(&acts) == nil {
				for _, act := range acts {
					n.actions = append(n.actions, act.Name)
				}
			}
		}
	}
	return ctx.Err()
}

// elementValueRead is how much of a field's text a read takes; reading one
// element's value takes more.
const elementValueRead = 2000

// wait waits for an in-flight call, or the context.
func wait(ctx context.Context, c *dbus.Call) *dbus.Call {
	select {
	case <-c.Done:
		return c
	case <-ctx.Done():
		return &dbus.Call{Err: ctx.Err()}
	}
}

// element turns a node into an element in monitor pixels and the AX roles
// the computer-use tools speak.
func (a *atspi) element(n *node, origin image.Point) Element {
	role, subrole := axRole(n)
	e := Element{
		Depth:   n.depth,
		Role:    role,
		Subrole: subrole,
		Title:   n.name,
		Value:   n.value,
		Enabled: n.has(stateSensitive) || n.has(stateEnabled),
		Focused: n.has(stateFocused),
		ref:     a.keep(n.ref),
	}
	if e.Title == "" {
		e.Title = n.desc
	}
	if e.secureField() {
		e.Value = ""
	}
	if !n.frame.Empty() {
		e.Frame = n.frame.Sub(origin)
	}
	seen := map[string]bool{}
	for _, act := range n.actions {
		name := axAction(act)
		if name != "" && !seen[name] {
			seen[name] = true
			e.Actions = append(e.Actions, name)
		}
	}
	return e
}

// axRoles name AT-SPI roles as the computer-use tools do on every computer.
var axRoles = map[string]string{
	"push button": "AXButton", "button": "AXButton", "toggle button": "AXButton",
	"check box": "AXCheckBox", "radio button": "AXRadioButton", "combo box": "AXComboBox",
	"menu item": "AXMenuItem", "check menu item": "AXMenuItem", "radio menu item": "AXMenuItem", "tearoff menu item": "AXMenuItem",
	"menu": "AXMenu", "popup menu": "AXMenu", "menu bar": "AXMenuBar", "link": "AXLink", "slider": "AXSlider",
	"spin button": "AXIncrementor", "page tab": "AXTab", "page tab list": "AXTabGroup",
	"list": "AXList", "list box": "AXList", "list item": "AXRow", "table": "AXTable", "table cell": "AXCell",
	"tree": "AXOutline", "tree table": "AXOutline", "tree item": "AXRow", "table row": "AXRow",
	"label": "AXStaticText", "static": "AXStaticText", "caption": "AXStaticText",
	"frame": "AXWindow", "window": "AXWindow", "dialog": "AXWindow", "alert": "AXWindow", "file chooser": "AXWindow",
	"tool bar": "AXToolbar", "scroll bar": "AXScrollBar", "image": "AXImage", "icon": "AXImage",
	"heading": "AXHeading", "progress bar": "AXProgressIndicator", "tool tip": "AXHelpTag",
	"panel": "AXGroup", "filler": "AXGroup", "grouping": "AXGroup", "section": "AXGroup", "form": "AXGroup",
	"viewport": "AXGroup", "scroll pane": "AXScrollArea", "split pane": "AXSplitGroup", "status bar": "AXGroup",
	"separator": "AXSplitter", "column header": "AXColumnHeader", "table column header": "AXColumnHeader",
	"row header": "AXRowHeader", "table row header": "AXRowHeader",
}

// axRole is a node's role, and a subrole where one says more. A password
// field is always AXSecureTextField, so its value is never read or shown.
func axRole(n *node) (string, string) {
	switch n.role {
	case "password text":
		return "AXSecureTextField", "AXSecureTextField"
	case "text", "entry", "editbar", "terminal", "paragraph", "document text", "document frame", "document web", "document email", "rich text":
		editable := n.has(stateEditable) || n.ifaces[atspiEditable]
		switch {
		case editable && (n.has(stateMultiLine) || strings.HasPrefix(n.role, "document") || n.role == "paragraph" || n.role == "terminal"):
			return "AXTextArea", ""
		case editable:
			return "AXTextField", ""
		case strings.HasPrefix(n.role, "document"):
			return "AXGroup", "AXDocument"
		}
		return "AXStaticText", ""
	case "dialog", "alert", "file chooser":
		return "AXWindow", "AXDialog"
	}
	if role, ok := axRoles[n.role]; ok {
		return role, ""
	}
	var b strings.Builder
	b.WriteString("AX")
	for _, word := range strings.Fields(n.role) {
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return b.String(), ""
}

// axAction names an element's own action as the tools do: press for what
// presses, clicks or activates it.
func axAction(name string) string {
	switch strings.ToLower(strings.ReplaceAll(name, " ", "")) {
	case "press", "click", "activate", "toggle", "jump", "open", "check", "uncheck", "select":
		return "AXPress"
	case "showmenu", "menu":
		return "AXShowMenu"
	case "setfocus", "focus", "":
		return "" // focus is every element's
	case "increase":
		return "AXIncrement"
	case "decrease":
		return "AXDecrement"
	case "scrolltovisible":
		return "AXScrollToVisible"
	}
	return name
}

// alive checks that a ref is still the live element it was.
func (a *atspi) alive(ctx context.Context, r atspiRef) error {
	var states []uint32
	if err := a.conn.Object(r.Bus, r.Path).CallWithContext(ctx, atspiAccessible+".GetState", 0).Store(&states); err != nil {
		return ErrStaleElement
	}
	if len(states) > 0 && states[0]&(1<<stateDefunct) != 0 {
		return ErrStaleElement
	}
	return nil
}

// act performs an action on a live element.
func (a *atspi) act(ctx context.Context, e Element, action, value string) error {
	r, ok := a.ref(e)
	if !ok {
		return ErrStaleElement
	}
	if err := a.alive(ctx, r); err != nil {
		return err
	}
	obj := a.conn.Object(r.Bus, r.Path)
	var done bool
	switch action {
	case "focus":
		if err := obj.CallWithContext(ctx, atspiComponent+".GrabFocus", 0).Store(&done); err != nil {
			return fmt.Errorf("focus: %w", err)
		}
		if !done {
			return errors.New("the app did not take the focus there")
		}
		return nil
	case "set_value":
		if e.Role == "AXSlider" || e.Role == "AXIncrementor" || e.Role == "AXProgressIndicator" {
			var f float64
			if _, err := fmt.Sscan(strings.TrimSpace(value), &f); err == nil {
				return obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0, atspiValue, "CurrentValue", dbus.MakeVariant(f)).Err
			}
		}
		if err := obj.CallWithContext(ctx, atspiEditable+".SetTextContents", 0, value).Store(&done); err != nil {
			return fmt.Errorf("this element's value can't be set here; focus it and type instead (%v)", err)
		}
		if !done {
			return errors.New("the app refused the value; focus the element and type instead")
		}
		return nil
	}
	var acts []struct{ Name, Desc, Key string }
	if err := obj.CallWithContext(ctx, atspiAction+".GetActions", 0).Store(&acts); err != nil {
		return fmt.Errorf("this element offers no actions; click it instead (%v)", err)
	}
	for i, act := range acts {
		if axAction(act.Name) == action || strings.EqualFold(act.Name, action) {
			if err := obj.CallWithContext(ctx, atspiAction+".DoAction", 0, int32(i)).Store(&done); err != nil {
				return err
			}
			if !done {
				return fmt.Errorf("the app did not do %s", strings.TrimPrefix(action, "AX"))
			}
			return nil
		}
	}
	return fmt.Errorf("this element doesn't do %s", strings.TrimPrefix(action, "AX"))
}

// value reads up to limit characters of an element's text, and its length.
func (a *atspi) value(ctx context.Context, e Element, limit int) (string, int, error) {
	r, ok := a.ref(e)
	if !ok {
		return "", 0, ErrStaleElement
	}
	if err := a.alive(ctx, r); err != nil {
		return "", 0, err
	}
	obj := a.conn.Object(r.Bus, r.Path)
	v, err := obj.GetProperty(atspiText + ".CharacterCount")
	if err != nil {
		return e.Value, len([]rune(e.Value)), nil
	}
	count, _ := v.Value().(int32)
	var text string
	if err := obj.CallWithContext(ctx, atspiText+".GetText", 0, int32(0), int32(min(int(count), limit))).Store(&text); err != nil {
		return "", 0, err
	}
	return text, int(count), nil
}

func (a *atspi) hash(e Element) uintptr {
	r, ok := a.ref(e)
	if !ok {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(r.Bus))
	h.Write([]byte{0})
	h.Write([]byte(r.Path))
	return uintptr(h.Sum64())
}

func (a *atspi) same(x, y Element) bool {
	rx, okx := a.ref(x)
	ry, oky := a.ref(y)
	return okx && oky && rx == ry
}

// atspiTimeout bounds one read of an app: a busy app is read in part.
var atspiTimeout = 4 * time.Second
