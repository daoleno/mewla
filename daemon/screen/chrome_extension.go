package screen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Mewla adds its own extension to the person's Chrome the way a person would,
// as an agent of its own on this screen: in a new tab, Chrome's extensions
// page, Developer mode on, Load unpacked, and the extension's folder chosen in
// the open panel. Chrome takes no other way from an app for an extension that
// is not in its store: it ignores chrome:// addresses handed to it, no longer
// loads one from its command line, and its profile is not Mewla's to read or
// write (macOS keeps one app out of another's data). The page and the panel
// are found by what does not change with Chrome's language: roles, the order
// of the page's toolbar, the focused field and keys; the folder goes into the panel's Go
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

	if err := act("open Chrome", AgentAction{Action: "open_app", App: chromeApp}); err != nil {
		return err
	}
	// Chrome shows an app its pages only while it thinks a screen reader
	// reads them: asked as VoiceOver asks, for as long as Mewla needs one.
	var before map[int]bool
	_ = waitFor("Chrome's windows", ElementMatch{Role: "AXWindow"}, "", 2)
	pid, err := s.internalPick(ChromeExtensionAgent, func(t ElementTree) (int, error) {
		before = windowIDs(t)
		return t.PID, nil
	})
	if err != nil {
		return err
	}
	screenReaderOff := func() {}
	if enhanced, ok := s.backend.(enhancedUISetter); ok && pid != 0 {
		if err := enhanced.SetEnhancedUI(pid, true); err != nil {
			log.Printf("screen: Chrome's pages as for a screen reader: %v", err)
		}
		screenReaderOff = sync.OnceFunc(func() {
			if err := enhanced.SetEnhancedUI(pid, false); err != nil {
				log.Printf("screen: Chrome back from screen reader mode: %v", err)
			}
		})
		defer screenReaderOff()
	}
	// A window of Mewla's own: the person's tabs stay as they are. It is the
	// one that was not there before, and Chrome's keys go to it only when
	// Chrome is in front as it opens (a window opened behind does not take
	// them, and keys go to the window that does).
	if err := act("Chrome in front", AgentAction{Action: "bring_forward", App: chromeApp}); err != nil {
		return err
	}
	if err := appKey("a new window", "cmd+n"); err != nil {
		return err
	}
	if err := s.untilRead(ctx, "Mewla's window", 5*time.Second, func(t ElementTree) error {
		_, _, err := newWindowAddress(t, before)
		return err
	}); err != nil {
		return fmt.Errorf("step %d: %w", step, err)
	}
	// It closes when Mewla is done, whatever happened, by its own close
	// button, while it is the one in front on the extensions page.
	defer func() {
		// Pages off first: a read of all of Chrome's then holds every window.
		screenReaderOff()
		if _, err := s.Act(ctx, ChromeExtensionAgent, AgentAction{Action: "wait_for", App: chromeApp, Match: &ElementMatch{Role: "AXWindow"}, Duration: 1}); err != nil {
			return
		}
		button, err := s.internalPick(ChromeExtensionAgent, func(t ElementTree) (int, error) { return closeButtonOfMewlas(t, before) })
		if err != nil {
			log.Printf("screen: Mewla's Chrome window stays open: %v", err)
			return
		}
		_, _ = s.Act(ctx, ChromeExtensionAgent, AgentAction{Action: "element", Element: button, ElementAction: "press"})
	}()
	// Its new tab page loads and may take the keyboard (the person's may be
	// any page): once it shows, the address bar takes it back, and the
	// extensions page's address goes in only while it has it.
	_ = s.untilRead(ctx, "Mewla's window's page", 5*time.Second, func(t ElementTree) error {
		start, end := frontWindow(t)
		for i := start; i < end; i++ {
			if t.Elements[i].Role == "AXWebArea" {
				return nil
			}
		}
		return errors.New("no page yet")
	})
	if err := pause(ctx, time.Second); err != nil {
		return err
	}
	const extensionsPage = "chrome://extensions/"
	for tries := 1; ; tries++ {
		var address int
		if err := s.untilRead(ctx, "Mewla's window's address bar", 3*time.Second, func(t ElementTree) error {
			var err error
			_, address, err = newWindowAddress(t, before)
			return err
		}); err != nil {
			return fmt.Errorf("step %d: %w", step, err)
		}
		if err := appKey("the address bar", "cmd+l"); err != nil {
			return err
		}
		if err := act("the extensions page's address", AgentAction{Action: "element", Element: address, ElementAction: "set_value", Text: extensionsPage}); err != nil {
			return err
		}
		// What Chrome adds to an address from the person's history goes;
		// the address is Mewla's.
		if err := appKey("only the extensions page's address", "delete"); err != nil {
			return err
		}
		ready := s.untilRead(ctx, "the extensions page's address", time.Second, func(t ElementTree) error {
			start, end := frontWindow(t)
			for i := start; i < end; i++ {
				if e := t.Elements[i]; e.Role == "AXTextField" {
					if !e.Focused || e.Value != extensionsPage {
						return fmt.Errorf("the address bar has %q and the keyboard %v", e.Value, e.Focused)
					}
					return nil
				}
			}
			return errors.New("no address bar")
		})
		if ready == nil {
			break
		}
		if tries == 3 {
			return fmt.Errorf("step %d: %w", step, ready)
		}
	}
	if err := appKey("open the extensions page", "Return"); err != nil {
		return err
	}
	var page extensionsToolbar
	if err := s.untilRead(ctx, "the extensions page", 15*time.Second, func(t ElementTree) error {
		var err error
		page, err = readExtensionsToolbar(t)
		return err
	}); err != nil {
		return fmt.Errorf("step %d (the extensions page): %w", step, err)
	}
	// Load unpacked shows only in Developer mode.
	if !page.developerMode {
		if err := act("turn on Developer mode", AgentAction{Action: "element", Element: page.devMode, ElementAction: "press"}); err != nil {
			return err
		}
		if err := s.untilRead(ctx, "Load unpacked", 5*time.Second, func(t ElementTree) error {
			var err error
			if page, err = readExtensionsToolbar(t); err == nil && !page.developerMode {
				err = errors.New("Developer mode is still off")
			}
			return err
		}); err != nil {
			return fmt.Errorf("step %d (Load unpacked): %w", step, err)
		}
	}
	if err := act("Load unpacked", AgentAction{Action: "element", Element: page.loadUnpacked, ElementAction: "press"}); err != nil {
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
	if err := s.untilRead(ctx, "the Go to Folder field", 4*time.Second, func(t ElementTree) error {
		_, err := focusedFieldInLastSheet(t)
		return err
	}); err != nil {
		return fmt.Errorf("step %d: %w", step, err)
	}
	if err := on("the extension's folder", focusedFieldInLastSheet, "set_value", dir); err != nil {
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

// enhancedUISetter sets an app's AXEnhancedUserInterface (macOS).
type enhancedUISetter interface {
	SetEnhancedUI(pid int, on bool) error
}

// extensionsToolbar is the toolbar of Chrome's extensions page, by its
// order, which is the same in every language: the search field, then the
// Developer mode switch, then, in Developer mode, Load unpacked first.
type extensionsToolbar struct {
	devMode, loadUnpacked int
	developerMode         bool
}

// readExtensionsToolbar finds the toolbar in the window in front (the first
// one read) once it shows the extensions page: its address is that page's,
// and no longer has the keyboard (an address being typed still shows the page
// before it, the person's new tab page).
func readExtensionsToolbar(t ElementTree) (extensionsToolbar, error) {
	start, end := frontWindow(t)
	address, page := -1, -1
	for i := start; i < end; i++ {
		if e := t.Elements[i]; e.Role == "AXTextField" && address < 0 {
			address = i
		} else if e.Role == "AXWebArea" {
			page = i
			break
		}
	}
	if page < 0 {
		return extensionsToolbar{}, errors.New("Chrome shows no page")
	}
	if address < 0 || !strings.HasPrefix(t.Elements[address].Value, "chrome://extensions") {
		return extensionsToolbar{}, fmt.Errorf("the window in front shows %q, not the extensions page", frontAddress(t))
	}
	if t.Elements[address].Focused {
		return extensionsToolbar{}, errors.New("the extensions page is still being opened")
	}
	search, devMode := -1, -1
	for i := page + 1; i < end && t.Elements[i].Depth > t.Elements[page].Depth; i++ {
		e := t.Elements[i]
		switch {
		case search < 0:
			if e.Role == "AXTextField" {
				search = i
			}
		case devMode < 0:
			if e.Role == "AXCheckBox" || e.Role == "AXSwitch" {
				devMode = i
				if e.Value != "1" {
					return extensionsToolbar{devMode: t.id(i)}, nil
				}
			}
		case e.Role == "AXButton":
			return extensionsToolbar{devMode: t.id(devMode), loadUnpacked: t.id(i), developerMode: true}, nil
		}
	}
	return extensionsToolbar{}, errors.New("the extensions page has no Developer mode switch yet")
}

// newWindowAddress is the window in front and its address bar when that
// window is not one of before.
func newWindowAddress(t ElementTree, before map[int]bool) (window, address int, err error) {
	start, end := frontWindow(t)
	if start >= end || t.Elements[start].Role != "AXWindow" {
		return 0, 0, errors.New("Chrome shows no window")
	}
	if before[t.id(start)] {
		return 0, 0, errors.New("no new window is in front yet")
	}
	for i := start; i < end; i++ {
		if t.Elements[i].Role == "AXTextField" {
			return t.id(start), t.id(i), nil
		}
	}
	return 0, 0, errors.New("the new window has no address bar yet")
}

// closeButtonOfMewlas is the close button of Mewla's window: the one in
// front, on the extensions page (typed or opened), not one of the windows
// before. A window's id does not always last a page change, so what it shows
// is what tells.
func closeButtonOfMewlas(t ElementTree, before map[int]bool) (int, error) {
	start, end := frontWindow(t)
	if start >= end || t.Elements[start].Role != "AXWindow" {
		return 0, errors.New("Chrome shows no window")
	}
	if before[t.id(start)] {
		return 0, errors.New("the window in front was there before Mewla's")
	}
	if a := frontAddress(t); !strings.HasPrefix(a, "chrome://extensions") {
		return 0, fmt.Errorf("the window in front shows %q", a)
	}
	for i := start; i < end; i++ {
		if t.Elements[i].Subrole == "AXCloseButton" {
			return t.id(i), nil
		}
	}
	return 0, errors.New("the window in front has no close button")
}

// frontWindow is where the browser window in front lies in a read: the first
// window with an address bar, and what follows it, deeper (the address bar's
// suggestions are a window of their own, in front of it); the whole read when
// it names no window.
func frontWindow(t ElementTree) (start, end int) {
	start, end = 0, len(t.Elements)
	first := true
	for i, e := range t.Elements {
		if e.Role != "AXWindow" {
			continue
		}
		stop := i + 1
		for stop < len(t.Elements) && t.Elements[stop].Depth > e.Depth {
			stop++
		}
		if first {
			start, end, first = i, stop, false
		}
		for j := i + 1; j < stop; j++ {
			if t.Elements[j].Role == "AXTextField" {
				return i, stop
			}
		}
	}
	return start, end
}

// frontAddress is the address of the tab in front: its window's address bar
// is the first text field in it.
func frontAddress(t ElementTree) string {
	start, end := frontWindow(t)
	for _, e := range t.Elements[start:end] {
		if e.Role == "AXTextField" {
			return e.Value
		}
	}
	return ""
}

// untilRead reads the app the internal agent acts on until check accepts a
// read, or says why the last one did not within d.
func (s *Service) untilRead(ctx context.Context, what string, d time.Duration, check func(ElementTree) error) error {
	deadline := time.Now().Add(d)
	for {
		_, err := s.internalPick(ChromeExtensionAgent, func(t ElementTree) (int, error) { return 0, check(t) })
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s: %w", what, err)
		}
		if err := pause(ctx, 500*time.Millisecond); err != nil {
			return err
		}
		if _, err := s.Act(ctx, ChromeExtensionAgent, AgentAction{Action: "wait_for", App: chromeApp, Match: &ElementMatch{Role: "AXWindow"}, Duration: 1}); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
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
