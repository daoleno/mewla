package screen

import (
	"context"
	"fmt"
	"image"
	"sort"
	"strings"
)

// The agent display is where the apps an agent works in wait: a display of
// its own, made when first needed, that the person's screen doesn't show. An
// app an agent acts on is moved there, so its windows never sit under the
// person's, never pop up over what they are doing, and keep drawing. They go
// back to where they were when the agent is done, stops, or the person takes
// over. Input reaches them the way it reaches any app behind others (see
// bgclick.go): to the app alone, with the pointer and the app in front left
// where they were.
//
// Without such a display (an older macOS, a refused window move) agents work
// as before, behind the person's windows, and the answer says so once.

// appParker is a backend that has an agent display.
type appParker interface {
	// AgentDisplayAvailable says whether this computer can have an agent
	// display at all (it may still fail to make one right now).
	AgentDisplayAvailable() bool
	// ParkApp moves the app's windows onto the agent display, making it when
	// needed. It is safe to call again: windows already there stay, and the
	// positions to go back to are kept from the first call.
	ParkApp(ctx context.Context, pid int) error
	// RestoreApp puts the app's windows back, and lets the display go when no
	// app is left on it.
	RestoreApp(pid int) error
	// ParkedOnDisplay says whether the app's windows are on the agent display
	// now (a display that slept or was lost takes them back).
	ParkedOnDisplay(pid int) bool
	// ParkedImage is the agent display's picture of rect, in global points.
	ParkedImage(ctx context.Context, rect image.Rectangle) (*image.RGBA, error)
}

// maybePark moves the app an action works on to the agent display, once. It
// returns a line to put before the answer, or "".
func (a *agentSession) maybePark(ctx context.Context, action AgentAction) string {
	parker, ok := a.apps.(appParker)
	if !ok || a.apps == nil || !parker.AgentDisplayAvailable() {
		return ""
	}
	name := action.App
	switch action.Action {
	case "left_click", "right_click", "middle_click", "double_click", "triple_click", "type", "key", "hold_key", "menu":
	case "element":
		a.appMu.Lock()
		if a.tree != nil {
			name = a.treeApp.Name
		}
		a.appMu.Unlock()
	default:
		return ""
	}
	if name == "" {
		return ""
	}
	apps, err := a.apps.Apps(ctx)
	if err != nil {
		return ""
	}
	app, err := resolveApp(apps, name)
	if err != nil || app.Active || app.Hidden {
		// The app in front is the person's, and an app with no window on a
		// screen has nothing to move.
		return ""
	}
	a.parkMu.Lock()
	defer a.parkMu.Unlock()
	if a.parked[app.PID] && parker.ParkedOnDisplay(app.PID) {
		return ""
	}
	if err := parker.ParkApp(ctx, app.PID); err != nil {
		delete(a.parked, app.PID)
		if a.parkTold {
			return ""
		}
		a.parkTold = true
		return fmt.Sprintf("No agent display here (%s), so %s stays behind your windows.", firstSentence(err.Error()), oneLine(app.Name))
	}
	if a.parked == nil {
		a.parked = map[int]bool{}
	}
	again := a.parked[app.PID]
	a.parked[app.PID] = true
	if again {
		return fmt.Sprintf("%s was back on your screen (the agent display went away, as it does when the Mac sleeps) and is on the agent display again.", oneLine(app.Name))
	}
	return fmt.Sprintf("%s is on the agent display, off your screen, until the agent is done. Coordinates for it are on its window screenshot (screenshot with app).", oneLine(app.Name))
}

// isParked says whether the app is on the agent display.
func (a *agentSession) isParked(pid int) bool {
	a.parkMu.Lock()
	defer a.parkMu.Unlock()
	return a.parked[pid]
}

// unpark puts one app back; bring_forward does it before raising the app.
func (a *agentSession) unpark(pid int) {
	a.parkMu.Lock()
	defer a.parkMu.Unlock()
	if !a.parked[pid] {
		return
	}
	if parker, ok := a.apps.(appParker); ok {
		_ = parker.RestoreApp(pid)
	}
	delete(a.parked, pid)
}

// unparkAll puts every parked app back: the agent is done.
func (a *agentSession) unparkAll() {
	a.parkMu.Lock()
	pids := make([]int, 0, len(a.parked))
	for pid := range a.parked {
		pids = append(pids, pid)
	}
	a.parkMu.Unlock()
	sort.Ints(pids)
	for _, pid := range pids {
		a.unpark(pid)
	}
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		s = s[:i]
	}
	return s
}

// agentDisplayHolder is a backend whose agent display can be held by
// something other than an app moved onto it: a browser window.
type agentDisplayHolder interface {
	HoldAgentDisplay(key string) (image.Rectangle, error)
	ReleaseAgentDisplay(key string)
}

// AgentDisplay is the way to hold the agent display, or false on a computer
// that has none.
func (s *Service) AgentDisplay() (hold func(key string) (image.Rectangle, error), release func(key string), ok bool) {
	holder, ok := s.backend.(agentDisplayHolder)
	if !ok {
		return nil, nil, false
	}
	if parker, isParker := s.backend.(appParker); isParker && !parker.AgentDisplayAvailable() {
		return nil, nil, false
	}
	return holder.HoldAgentDisplay, holder.ReleaseAgentDisplay, true
}
