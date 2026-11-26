package screen

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Mewla adds its own extension to the person's Chrome the way a person would,
// as an agent of its own on this screen: in a new tab, Chrome's extensions
// page, Developer mode on, Load unpacked, and the extension's folder chosen in
// the open panel. Chrome takes no other way from an app for an extension that
// is not in its store: it ignores chrome:// addresses handed to it, no longer
// loads one from its command line, and its profile is not Mewla's to read or
// write (macOS keeps one app out of another's data). The page and the panel
// are found by what does not change with Chrome's language: the page's element
// ids, roles, the focused field and keys; the folder goes into the panel's Go
// to Folder field as its value, never typed (an input method would take
// typed keys). The person sees Mewla using the screen and can stop it.

// ChromeExtensionAgent is the agent Mewla is while it adds its extension.
const ChromeExtensionAgent = "mewla:chrome-extension"

// internalAgents are agents of Mewla's own, no Worker's, with their names.
var internalAgents = map[string]string{ChromeExtensionAgent: "Mewla, adding its Chrome extension"}

// errAgentDone ends an internal agent's control once its work is done: its
// apps go back to where they were at once.
var errAgentDone = errors.New("Mewla is done with the screen")

const chromeApp = "Google Chrome"

// The extensions page's own element ids (chrome://extensions, its toolbar).
const (
	chromeDevModeID      = "devMode"
	chromeLoadUnpackedID = "loadUnpacked"
)

// LoadUnpackedInChrome adds the extension in dir to the person's Chrome and
// returns once loaded() says it is there, or why it could not be done.
func (s *Service) LoadUnpackedInChrome(ctx context.Context, dir string, loaded func() bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	defer s.finishInternalAgent(ChromeExtensionAgent)
	step := 0
	act := func(what string, action AgentAction) error {
		step++
		if _, err := s.Act(ctx, ChromeExtensionAgent, action); err != nil {
			return fmt.Errorf("step %d (%s): %w", step, what, err)
		}
		return nil
	}
	// Keys for Chrome: its menus take the shortcuts they have.
	appKey := func(what, keys string) error {
		return act(what, AgentAction{Action: "key", App: chromeApp, Text: keys})
	}
	// Keys as a person types them, to the app in front: the open panel's
	// own shortcuts, which Chrome's menus would take otherwise.
	key := func(what, keys string) error { return act(what, AgentAction{Action: "key", Text: keys}) }
	waitFor := func(what string, match ElementMatch, until string, seconds float64) error {
		return act(what, AgentAction{Action: "wait_for", App: chromeApp, Match: &match, Until: until, Duration: seconds})
	}
	// An element of the latest read: the first one found, which is in the
	// window in front (Mewla's own), or the one pick chooses.
	on := func(what string, pick func(ElementTree) (int, error), action, value string) error {
		id, err := s.internalPick(ChromeExtensionAgent, pick)
		if err != nil {
			return fmt.Errorf("step %d (%s): %w", step+1, what, err)
		}
		return act(what, AgentAction{Action: "element", Element: id, ElementAction: action, Text: value})
	}
	first := func(m ElementMatch) func(ElementTree) (int, error) {
		return func(t ElementTree) (int, error) { return t.match(m, false) }
	}

	if err := act("open Chrome", AgentAction{Action: "open_app", App: chromeApp}); err != nil {
		return err
	}
	// A window of Mewla's own: the person's tabs stay as they are. Its
	// address bar, empty, has the keyboard.
	if err := appKey("a new window", "cmd+n"); err != nil {
		return err
	}
	address := ElementMatch{Role: "AXTextField", Focused: true, Empty: true}
	if err := waitFor("the new window's address bar", address, "", 5); err != nil {
		return err
	}
	if err := on("the extensions page's address", first(address), "set_value", "chrome://extensions/"); err != nil {
		return err
	}
	if err := appKey("open the extensions page", "Return"); err != nil {
		return err
	}
	devMode := ElementMatch{DOMID: chromeDevModeID}
	if err := waitFor("the extensions page", devMode, "", 10); err != nil {
		return err
	}
	loadUnpacked := ElementMatch{DOMID: chromeLoadUnpackedID}
	// Load unpacked shows only in Developer mode; pressed while it shows, the
	// switch would turn it off.
	if _, err := s.internalPick(ChromeExtensionAgent, first(loadUnpacked)); err != nil {
		if err := on("turn on Developer mode", first(devMode), "press", ""); err != nil {
			return err
		}
		if err := waitFor("Load unpacked", loadUnpacked, "enabled", 5); err != nil {
			return err
		}
	}
	if err := on("Load unpacked", first(loadUnpacked), "press", ""); err != nil {
		return err
	}
	// The open panel, a sheet on Chrome's window; its Go to Folder field.
	sheet := ElementMatch{Role: "AXSheet"}
	if err := waitFor("the open panel", sheet, "", 10); err != nil {
		return err
	}
	if err := act("Chrome in front", AgentAction{Action: "bring_forward", App: chromeApp}); err != nil {
		return err
	}
	if err := key("Go to Folder", "cmd+shift+g"); err != nil {
		return err
	}
	if err := waitFor("the Go to Folder field", ElementMatch{Role: "AXSheet"}, "", 5); err != nil {
		return err
	}
	var field func(ElementTree) (int, error) = focusedFieldInLastSheet
	for tries := 0; ; tries++ {
		if _, err := s.internalPick(ChromeExtensionAgent, field); err == nil || tries == 10 {
			break
		}
		if err := waitFor("the Go to Folder field", sheet, "", 1); err != nil {
			return err
		}
		if err := pause(ctx, 300*time.Millisecond); err != nil {
			return err
		}
	}
	if err := on("the extension's folder", field, "set_value", dir); err != nil {
		return err
	}
	if err := key("go to the folder", "Return"); err != nil {
		return err
	}
	// The panel now shows the folder: its default button chooses it.
	if err := pause(ctx, time.Second); err != nil {
		return err
	}
	if err := key("choose the folder", "Return"); err != nil {
		return err
	}
	if err := waitFor("the open panel closes", sheet, "gone", 10); err != nil {
		return err
	}
	// Chrome starts the extension, which connects to Mewla.
	deadline := time.Now().Add(30 * time.Second)
	for !loaded() {
		if time.Now().After(deadline) {
			return errors.New("Chrome took the folder but the extension did not connect to Mewla within 30 seconds")
		}
		if err := pause(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	// Mewla's window is done.
	_ = appKey("close Mewla's window", "cmd+shift+w")
	return nil
}

func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// focusedFieldInLastSheet is the text field with the keyboard in the sheet
// opened last (the open panel's Go to Folder): a sheet's elements follow it
// in a read, deeper than it.
func focusedFieldInLastSheet(t ElementTree) (int, error) {
	last := -1
	for i, e := range t.Elements {
		if e.Role == "AXSheet" {
			last = i
		}
	}
	if last < 0 {
		return 0, errors.New("no sheet is open")
	}
	for i := last + 1; i < len(t.Elements) && t.Elements[i].Depth > t.Elements[last].Depth; i++ {
		e := t.Elements[i]
		if e.Focused && (e.Role == "AXTextField" || e.Role == "AXComboBox") {
			return t.id(i), nil
		}
	}
	return 0, errors.New("the sheet's field does not have the keyboard")
}

// internalPick chooses an element of an internal agent's latest read.
func (s *Service) internalPick(worker string, pick func(ElementTree) (int, error)) (int, error) {
	s.agentMu.Lock()
	agent := s.agents[worker]
	s.agentMu.Unlock()
	if agent == nil {
		return 0, errors.New("Mewla is not using the screen")
	}
	agent.appMu.Lock()
	defer agent.appMu.Unlock()
	if agent.tree == nil {
		return 0, errors.New("nothing read yet")
	}
	return pick(*agent.tree)
}

// finishInternalAgent ends an internal agent's control: its apps go back to
// where they were at once, not after an idle agent's lease.
func (s *Service) finishInternalAgent(worker string) {
	s.agentMu.Lock()
	agent := s.agents[worker]
	s.agentMu.Unlock()
	if agent != nil {
		agent.cancel(errAgentDone)
	}
}
