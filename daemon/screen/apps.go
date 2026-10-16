package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// App is one running app with a window, as an agent targets it.
type App struct {
	Name string `json:"name"`
	PID  int    `json:"pid"`
	// Active is the app in front; Hidden has no window on screen.
	Active bool `json:"active,omitempty"`
	Hidden bool `json:"hidden,omitempty"`
	// Window is its front window on the screen, in framebuffer pixels;
	// CoveredBy names the apps whose windows lie over part of it.
	Window    image.Rectangle `json:"-"`
	CoveredBy []string        `json:"-"`
	// Windows are its windows on the screen, front to back.
	Windows []AppWindow `json:"-"`
	// Chromium apps (Chrome, Edge, Electron apps) ignore input they can't
	// trust, so they are only clicked on the screen.
	Chromium bool `json:"-"`
}

// AppWindow is one window on the screen, in framebuffer pixels.
type AppWindow struct {
	ID     uint32
	Bounds image.Rectangle
}

// appClicker is a session that can click in an app's window behind others,
// without the pointer.
type appClicker interface {
	// BackgroundClicks says whether it can on this computer, and if not,
	// why not.
	BackgroundClicks() (bool, string)
	ClickTo(pid int, window uint32, x, y, button, clicks int) error
}

// appOpener is a backend that can open an app that isn't running, behind
// the one in front where the system allows.
type appOpener interface {
	OpenApp(ctx context.Context, name string) error
}

// bundleID is how an app's bundle id looks, such as com.apple.Notes.
var bundleID = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+){2,}$`)

// checkAppName refuses anything but an app's name or bundle id: open_app
// never opens a file, a URL or a command-line option.
func checkAppName(name string) error {
	if name == "" {
		return errors.New("open_app needs an app's name, such as Notes, or its bundle id")
	}
	if strings.ContainsAny(name, "/\\:") || strings.HasPrefix(name, "-") || strings.HasPrefix(name, ".") ||
		strings.IndexFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
		return fmt.Errorf("open_app opens an app by its name or bundle id, not a file, URL or option: %q", name)
	}
	return nil
}

// appPresser is a backend that can press, through accessibility, what an
// app has at a point, behind other windows. pressed is false when nothing
// there can be pressed.
type appPresser interface {
	PressAt(ctx context.Context, pid, x, y int) (what string, pressed bool, err error)
}

// elementIdentity is a backend that can tell an element in a new read is
// the same live element as one before, which then keeps its id.
type elementIdentity interface {
	ElementHash(e Element) uintptr
	SameElement(a, b Element) bool
}

// appRaiser is a backend that can bring an app to the front.
type appRaiser interface {
	RaiseApp(ctx context.Context, pid int) error
}

// appReader is a backend that can list apps and read any one app's windows
// as elements an agent can act on, in front or not.
type appReader interface {
	Apps(ctx context.Context) ([]App, error)
	// ReadApp reads every window of the app with pid. Each element keeps a
	// handle to the app's live element until Release.
	ReadApp(ctx context.Context, pid int, limit int) (ElementTree, error)
	Release(tree ElementTree)
}

// appActor is a backend that can act on an element an app read returned.
type appActor interface {
	// ActOnElement checks that e is still the element that was read, then
	// performs action: an accessibility action name, "focus", or
	// "set_value" with value.
	ActOnElement(ctx context.Context, e Element, action, value string) error
}

// ErrStaleElement means an id no longer names what was read.
var ErrStaleElement = errors.New("That element changed or went away; read the app again with app_state")

// elementActions are the actions an agent names, and what each does.
var elementActions = map[string]string{
	"press": "AXPress", "show_menu": "AXShowMenu", "scroll_to_visible": "AXScrollToVisible",
	"raise": "AXRaise", "confirm": "AXConfirm", "cancel": "AXCancel", "increment": "AXIncrement",
	"decrement": "AXDecrement", "pick": "AXPick", "focus": "focus", "set_value": "set_value",
}

// elementAction resolves what an agent asked for on e: a standard action, or
// one of the element's own actions by name, with or without "AX".
func elementAction(e Element, asked string) (string, error) {
	asked = strings.TrimSpace(asked)
	if action, ok := elementActions[strings.ToLower(asked)]; ok {
		return action, nil
	}
	for _, own := range e.Actions {
		if strings.EqualFold(own, asked) || strings.EqualFold(strings.TrimPrefix(own, "AX"), asked) {
			return own, nil
		}
	}
	return "", fmt.Errorf("%q is not something this element does; it offers press, focus, set_value and its own actions", asked)
}

// ErrNoApps means this computer can't read apps as elements.
var ErrNoApps = errors.New("Reading apps works on Macs; use screenshots here")

// resolveApp finds the one app a name or pid means. A name matches without
// case; a name several apps share is an error that lists them.
func resolveApp(apps []App, name string) (App, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		for _, app := range apps {
			if app.Active {
				return app, nil
			}
		}
		return App{}, errors.New("no app is in front; name one from apps")
	}
	if pid, err := strconv.Atoi(name); err == nil {
		for _, app := range apps {
			if app.PID == pid {
				return app, nil
			}
		}
		return App{}, fmt.Errorf("no app with a window has pid %d; see apps", pid)
	}
	var found []App
	for _, app := range apps {
		if strings.EqualFold(app.Name, name) {
			found = append(found, app)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return App{}, fmt.Errorf("no app named %q has a window; see apps", name)
	}
	pids := make([]string, len(found))
	for i, app := range found {
		pids[i] = strconv.Itoa(app.PID)
	}
	return App{}, fmt.Errorf("%d apps are named %q; name one by pid: %s", len(found), name, strings.Join(pids, ", "))
}

func formatApps(apps []App) string {
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Active != apps[j].Active {
			return apps[i].Active
		}
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
	var b strings.Builder
	b.WriteString("Apps with a window (name, pid). Read one with app_state.\n")
	for _, app := range apps {
		line := fmt.Sprintf("%s, pid %d", oneLine(app.Name), app.PID)
		if app.Active {
			line += ", in front"
		}
		if app.Hidden {
			line += ", hidden"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// Actions most elements offer; they alone don't make an element worth a line.
var commonActions = map[string]bool{"AXScrollToVisible": true, "AXShowMenu": true}

// formatAppTree lists an app's elements with the ids an agent acts on. An
// element on the screen also gets the point to click, in screenshot pixels.
func formatAppTree(app App, tree ElementTree, screen image.Rectangle, toAgent func(image.Point) (int, int)) string {
	var b strings.Builder
	fmt.Fprintf(&b, "App: %s, pid %d", oneLine(app.Name), app.PID)
	if app.Active {
		b.WriteString(", in front")
	}
	b.WriteString(".\nEach line: [id] role \"name\" = \"value\" {actions} at (x, y). Act on an element by its id; an element keeps its id while it stays the same, and an id whose element is gone is refused.\n")
	shown, hidden := 0, 0
	for i, e := range tree.Elements {
		title, value := oneLine(e.Title), oneLine(e.Value)
		if e.Role == "AXSecureTextField" {
			value = ""
		}
		special := false
		for _, action := range e.Actions {
			special = special || !commonActions[action]
		}
		if title == "" && value == "" && !actionableRoles[e.Role] && !special && e.Role != "AXWindow" {
			continue
		}
		if shown == elementsShown {
			hidden++
			continue
		}
		shown++
		line := strings.Repeat("  ", min(e.Depth, 8)) + fmt.Sprintf("[%d] %s", tree.id(i), strings.TrimPrefix(e.Role, "AX"))
		if title != "" {
			line += ` "` + title + `"`
		}
		if value != "" {
			line += ` = "` + value + `"`
		}
		if len(e.Actions) > 0 {
			names := make([]string, len(e.Actions))
			for j, action := range e.Actions {
				names[j] = strings.TrimPrefix(action, "AX")
			}
			line += " {" + strings.Join(names, ", ") + "}"
		}
		if !e.Enabled {
			line += " (disabled)"
		}
		if e.Focused {
			line += " (focused)"
		}
		if !e.Frame.Empty() && e.Frame.Overlaps(screen) {
			x, y := toAgent(image.Pt((e.Frame.Min.X+e.Frame.Max.X)/2, (e.Frame.Min.Y+e.Frame.Max.Y)/2))
			line += fmt.Sprintf(" at (%d, %d)", x, y)
		}
		b.WriteString(line + "\n")
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "(%d more not shown)\n", hidden)
	}
	if tree.Truncated {
		b.WriteString("(the app has more than was read)\n")
	}
	if shown == 0 {
		b.WriteString("Nothing readable: this app shows little to accessibility; use screenshots.\n")
	}
	return b.String()
}
