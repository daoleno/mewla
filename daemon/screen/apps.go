package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
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
	b.WriteString(".\nEach line: [id] role \"name\" = \"value\" {actions} at (x, y). Act on an element by its id; ids last until this agent reads an app again.\n")
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
		line := strings.Repeat("  ", min(e.Depth, 8)) + fmt.Sprintf("[%d] %s", i+1, strings.TrimPrefix(e.Role, "AX"))
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
