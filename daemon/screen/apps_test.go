package screen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func fakeApps() []FakeApp {
	return []FakeApp{
		{App: App{Name: "Notes", PID: 101, Active: true}, Elements: []Element{
			{Role: "AXWindow", Title: "Notes", Frame: image.Rect(0, 0, 2000, 1400), Enabled: true},
			{Depth: 1, Role: "AXButton", Title: "New Note", Frame: image.Rect(100, 100, 200, 140), Enabled: true, Actions: []string{"AXPress", "AXShowMenu"}},
			{Depth: 1, Role: "AXGroup", Enabled: true, Actions: []string{"AXScrollToVisible"}},
			{Depth: 1, Role: "AXSecureTextField", Title: "Password", Value: "hunter2", Frame: image.Rect(100, 200, 400, 240), Enabled: true, Actions: []string{"AXConfirm"}},
		}},
		{App: App{Name: "Safari", PID: 202, Hidden: true}, Elements: []Element{
			{Role: "AXWindow", Title: "Bank", Enabled: true},
			{Depth: 1, Role: "AXLink", Title: "Statements", Frame: image.Rect(-900, 0, -800, 20), Enabled: true, Actions: []string{"AXPress"}},
		}},
		{App: App{Name: "Helper", PID: 301}},
		{App: App{Name: "helper", PID: 302}},
	}
}

// Apps in the background are read without bringing them forward, each
// element with an id, and the handles behind the ids never pile up.
func TestAgentReadsAnyAppWithIds(t *testing.T) {
	s, backend, dir := newAgentService(t)
	backend.RunningApps = fakeApps()

	list := act(t, s, "%7", AgentAction{Action: "apps"}).Text
	if !strings.Contains(list, "Notes, pid 101, in front\n") || !strings.Contains(list, "Safari, pid 202, hidden\n") || strings.Index(list, "Notes") > strings.Index(list, "Safari") {
		t.Fatalf("apps:\n%s", list)
	}

	notes := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	for _, want := range []string{"App: Notes, pid 101, in front.", `[2] Button "New Note" {Press, ShowMenu} at (75, 60)`, `[4] SecureTextField "Password" {Confirm} at (125, 110)`} {
		if !strings.Contains(notes, want) {
			t.Fatalf("missing %q in:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "hunter2") || strings.Contains(notes, "[3]") {
		t.Fatalf("a password or an empty group shown:\n%s", notes)
	}

	safari := act(t, s, "%7", AgentAction{Action: "app_state", App: "safari"}).Text
	if !strings.Contains(safari, `[6] Link "Statements" {Press}`+"\n") {
		t.Fatalf("an element off the screen still has an id, without a point:\n%s", safari)
	}
	if backend.Retained != 2 {
		t.Fatalf("the earlier read's handles were kept: %d", backend.Retained)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "app_state", App: "HELPER"}); err == nil || !strings.Contains(err.Error(), "301, 302") {
		t.Fatalf("ambiguous name: %v", err)
	}
	if got := act(t, s, "%7", AgentAction{Action: "app_state", App: "302"}).Text; !strings.Contains(got, "App: helper, pid 302.") || !strings.Contains(got, "Nothing readable") {
		t.Fatalf("by pid:\n%s", got)
	}

	s.StopAgent()
	waitFor(t, func() bool {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		return backend.Retained == 0
	})
	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if !strings.Contains(string(audit), `"action":"app_state","app":"safari"`) {
		t.Fatalf("audit:\n%s", audit)
	}
}

// Acting by id works on an app behind others, refuses an element that has
// changed since it was read, and keeps what was set out of the audit.
func TestAgentActsOnElementsById(t *testing.T) {
	s, backend, dir := newAgentService(t)
	apps := fakeApps()
	apps[1].Elements = append(apps[1].Elements, Element{Depth: 1, Role: "AXTextField", Title: "Search", Enabled: true, Actions: []string{"AXConfirm"}})
	backend.RunningApps = apps
	element := func(id int, does, text string) (AgentResult, error) {
		return s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: id, ElementAction: does, Text: text})
	}
	if _, err := element(1, "press", ""); err == nil || !strings.Contains(err.Error(), "app_state first") {
		t.Fatalf("before reading: %v", err)
	}

	act(t, s, "%7", AgentAction{Action: "app_state", App: "Safari"})
	result, err := element(3, "set_value", "statements 2026")
	if err != nil || !strings.Contains(result.Text, "Done: set_value on [3]") || !strings.Contains(result.Text, `[3] TextField "Search" = "statements 2026"`) {
		t.Fatalf("set value: %v\n%s", err, result.Text)
	}
	if _, err := element(3, "Confirm", ""); err != nil {
		t.Fatalf("the element's own action: %v", err)
	}
	if _, err := element(2, "press", ""); err != nil {
		t.Fatalf("press: %v", err)
	}
	want := []FakeElementAction{{202, 2, "set_value", "statements 2026"}, {202, 2, "AXConfirm", ""}, {202, 1, "AXPress", ""}}
	if len(backend.ElementActions) != 3 || backend.ElementActions[0] != want[0] || backend.ElementActions[1] != want[1] || backend.ElementActions[2] != want[2] {
		t.Fatalf("actions %+v", backend.ElementActions)
	}
	if _, err := element(2, "delete_everything", ""); err == nil || !strings.Contains(err.Error(), "not something this element does") {
		t.Fatalf("unknown action: %v", err)
	}
	if _, err := element(9, "press", ""); err == nil || !strings.Contains(err.Error(), "no element [9]") {
		t.Fatalf("unknown id: %v", err)
	}

	// The page changed under the agent: the old id is refused, not guessed.
	backend.mu.Lock()
	backend.RunningApps[1].Elements[1].Title = "Transfer money"
	backend.mu.Unlock()
	if _, err := element(2, "press", ""); !errors.Is(err, ErrStaleElement) {
		t.Fatalf("stale: %v", err)
	}
	if len(backend.ElementActions) != 3 {
		t.Fatal("acted on a changed element")
	}

	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if strings.Contains(string(audit), "statements 2026") || !strings.Contains(string(audit), `"element":3,"element_action":"set_value","text_length":15`) {
		t.Fatalf("audit:\n%s", audit)
	}
}

// Keys and text can go to an app behind others, without touching the one in
// front; pointer actions can't, since only keys reach one app.
func TestAgentTypesIntoAnAppBehindOthers(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = fakeApps()
	if got := act(t, s, "%7", AgentAction{Action: "type", Text: "hi", App: "Safari"}).Text; !strings.Contains(got, "app_state") {
		t.Fatalf("type: %q", got)
	}
	act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+l", App: "202"})
	backend.mu.Lock()
	keys, global := backend.AppKeys, len(backend.Keys)
	backend.mu.Unlock()
	if global != 0 || len(keys) != 8 {
		t.Fatalf("keys to the app %+v, to the computer %d", keys, global)
	}
	for _, k := range keys {
		if k.PID != 202 {
			t.Fatalf("a key went elsewhere: %+v", keys)
		}
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "mouse_move", Coordinate: []int{1, 1}, App: "Safari"}); err == nil || !strings.Contains(err.Error(), "only clicks, type, key and hold_key") {
		t.Fatalf("click in an app: %v", err)
	}
	// The agent's own keys to the app are not a person at the computer.
	act(t, s, "%7", AgentAction{Action: "type", Text: "more", App: "Safari"})
}

// One app's window, at the scale of a full screenshot and placed on it; a
// window under another says so, and a hidden one can't be pictured.
func TestAgentSeesOneAppsWindow(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Window = image.Rect(0, 0, 1000, 800)
	apps[2].Window = image.Rect(400, 300, 1400, 1100)
	apps[2].CoveredBy = []string{"Notes"}
	backend.RunningApps = apps
	shot := act(t, s, "%7", AgentAction{Action: "screenshot", App: "301"})
	if shot.Width != 500 || shot.Height != 400 || len(shot.Image) == 0 ||
		!strings.Contains(shot.Text, "Its top left is (200, 150)") || !strings.Contains(shot.Text, "under Notes") {
		t.Fatalf("window %dx%d: %s", shot.Width, shot.Height, shot.Text)
	}
	if shot := act(t, s, "%7", AgentAction{Action: "screenshot", App: "Notes"}); strings.Contains(shot.Text, "under") {
		t.Fatalf("front window: %s", shot.Text)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot", App: "Safari"}); err == nil || !strings.Contains(err.Error(), "no window on the screen") {
		t.Fatalf("hidden: %v", err)
	}
}

// While someone uses the computer, work on an app behind theirs goes on; the
// screen itself and the app in front wait for them. Stop still ends it all.
func TestAPersonPausesOnlyWhatTheyUse(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[1].Window = image.Rect(0, 0, 400, 300)
	backend.RunningApps = apps
	act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"})
	backend.PersonInput()

	paused := []AgentAction{
		{Action: "left_click", Coordinate: []int{10, 10}},
		{Action: "type", Text: "x"},
		{Action: "screenshot"},
		{Action: "app_state"},
		{Action: "element", Element: 2, ElementAction: "press"},
		{Action: "type", Text: "x", App: "Notes"},
	}
	for _, action := range paused {
		if _, err := s.Act(context.Background(), "%7", action); !errors.Is(err, ErrPersonAtComputer) {
			t.Fatalf("%s %s went on while someone used the computer: %v", action.Action, action.App, err)
		}
	}
	if len(backend.ElementActions) != 0 {
		t.Fatal("pressed in the app in front")
	}

	act(t, s, "%7", AgentAction{Action: "apps"})
	safari := act(t, s, "%7", AgentAction{Action: "app_state", App: "Safari"}).Text
	act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, safari, `Link "Statements"`), ElementAction: "press"})
	act(t, s, "%7", AgentAction{Action: "type", Text: "rent", App: "Safari"})
	act(t, s, "%7", AgentAction{Action: "hold_key", Text: "shift", Duration: 0.3, App: "Safari"})
	act(t, s, "%7", AgentAction{Action: "screenshot", App: "Safari"})
	if result, err := s.Act(context.Background(), "%7", AgentAction{Action: "steps", Steps: []AgentAction{
		{Action: "wait_for", App: "Safari", Match: &ElementMatch{Title: "Statements"}},
		{Action: "element", Match: &ElementMatch{Title: "Statements"}, ElementAction: "press"},
	}}); err != nil {
		t.Fatalf("steps behind: %v\n%s", err, result.Text)
	}
	if len(backend.ElementActions) != 2 {
		t.Fatalf("background presses %+v", backend.ElementActions)
	}

	// The person is still first: once it waited, the screen still waits.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{10, 10}}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("after background work: %v", err)
	}
	// Stop ends background work too.
	s.StopAgent()
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	backend.SetLocked(true)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: 2, ElementAction: "press"}); !errors.Is(err, ErrScreenLocked) {
		t.Fatalf("locked: %v", err)
	}
}

// A click names an app: behind other windows when SkyLight can, which
// leaves the pointer alone and goes on while someone works; otherwise on the
// screen, after bringing the app forward, and paused for a person. The
// answer says which.
func TestClicksReachAnAppBehindOthers(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[1].Hidden = false
	apps[1].Windows = []AppWindow{{ID: 7, Bounds: image.Rect(0, 0, 400, 300)}}
	apps = append(apps, FakeApp{App: App{Name: "Slack", PID: 404, Chromium: true, Windows: []AppWindow{{ID: 9, Bounds: image.Rect(1000, 0, 1400, 300)}}}})
	backend.RunningApps = apps
	backend.SkyLight = true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	pointers, _ := backend.Snapshot()

	backend.PersonInput()
	if got := act(t, s, "%7", AgentAction{Action: "double_click", Coordinate: []int{100, 100}, App: "Safari"}).Text; !strings.Contains(got, "path: SkyLight") {
		t.Fatalf("behind: %s", got)
	}
	if c := backend.AppClicks; len(c) != 1 || c[0] != (FakeAppClick{202, 7, 200, 200, 0, 2}) {
		t.Fatalf("clicks %+v", c)
	}
	if after, _ := backend.Snapshot(); len(after) != len(pointers) || len(backend.Raised) != 0 {
		t.Fatal("the pointer moved or the app came forward")
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{300, 300}, App: "Safari"}); err == nil || !strings.Contains(err.Error(), "not on a window of Safari") {
		t.Fatalf("off its window: %v", err)
	}

	// Chromium, or no SkyLight: on the screen, so the person comes first.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{600, 100}, App: "Slack"}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("on the screen while someone works: %v", err)
	}
	backend.ForgetInput()
	s.StopAgent()
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	got := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{600, 100}, App: "Slack"}).Text
	if !strings.Contains(got, "path: screen pointer, after bringing Slack to the front") || !strings.Contains(got, "Chromium") {
		t.Fatalf("chromium: %s", got)
	}
	if after, _ := backend.Snapshot(); len(after) == len(pointers) || after[len(after)-1] != (FakePointer{1200, 200, 0}) || backend.Raised[0] != 404 {
		t.Fatalf("screen click %v raised %v", after[len(pointers):], backend.Raised)
	}

	backend.mu.Lock()
	backend.SkyLight = false
	backend.mu.Unlock()
	if got := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 100}, App: "Safari"}).Text; !strings.Contains(got, "SkyLight isn't available") || len(backend.AppClicks) != 1 {
		t.Fatalf("no SkyLight: %s", got)
	}
	// Safari is in front now: a click there is a plain screen click.
	if got := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 100}, App: "Safari"}).Text; !strings.Contains(got, "Safari is in front") || len(backend.Raised) != 2 {
		t.Fatalf("in front: %s %v", got, backend.Raised)
	}
}

// An app that isn't running opens behind the one in front, by name or
// bundle id, even while someone uses the computer; only apps open.
func TestOpenAnAppThatIsNotRunning(t *testing.T) {
	s, backend, dir := newAgentService(t)
	backend.RunningApps = fakeApps()
	backend.InstalledApps = []FakeInstalledApp{
		{FakeApp: FakeApp{App: App{Name: "提醒事项", PID: 505, Bundle: "Reminders"}}, Bundle: "com.apple.reminders"},
		{FakeApp: FakeApp{App: App{Name: "Calendar", PID: 606}}},
	}
	act(t, s, "%7", AgentAction{Action: "apps"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()

	if got := act(t, s, "%7", AgentAction{Action: "open_app", App: "com.apple.reminders"}).Text; got != "Opened 提醒事项 (pid 505) behind the app in front; read it with app_state." {
		t.Fatalf("by bundle id: %s", got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "open_app", App: "calendar"}).Text; !strings.Contains(got, "Opened Calendar (pid 606)") {
		t.Fatalf("by name: %s", got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "open_app", App: "Calendar"}).Text; got != "Calendar is already open (pid 606); read it with app_state." {
		t.Fatalf("again: %s", got)
	}
	// A Mac in Chinese shows Reminders as 提醒事项: it is known by both names.
	if got := act(t, s, "%7", AgentAction{Action: "open_app", App: "reminders"}).Text; got != "提醒事项 is already open (pid 505); read it with app_state." {
		t.Fatalf("by its English name: %s", got)
	}
	act(t, s, "%7", AgentAction{Action: "app_state", App: "提醒事项"})
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "open_app", App: "Nope"}); err == nil || !strings.Contains(err.Error(), "Unable to find application") {
		t.Fatalf("missing: %v", err)
	}
	for _, name := range []string{"", "/Applications/Notes.app", "https://example.com", "-n", "../x", "a\nb", "file:x"} {
		if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "open_app", App: name}); err == nil || errors.Is(err, ErrPersonAtComputer) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if want := []string{"com.apple.reminders", "calendar", "Nope"}; !slices.Equal(backend.Launched, want) {
		t.Fatalf("launched %q", backend.Launched)
	}
	if pointers, keys := backend.Snapshot(); len(pointers)+len(keys) != 0 {
		t.Fatal("opening an app touched the pointer or keyboard")
	}
	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if strings.Count(string(audit), `"action":"open_app"`) != 12 {
		t.Fatalf("audit %s", audit)
	}
}

// idOf is the id on the line of an app_state that holds label.
func idOf(t *testing.T, state, label string) int {
	t.Helper()
	for _, line := range strings.Split(state, "\n") {
		if strings.Contains(line, label) {
			var id int
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "[%d]", &id); err == nil {
				return id
			}
		}
	}
	t.Fatalf("no %q in:\n%s", label, state)
	return 0
}

// An element keeps its id while it is the same element; an id whose
// element went away is refused, never moved to whatever took its place.
func TestAnIdNeverMovesToAnotherElement(t *testing.T) {
	s, backend, _ := newAgentService(t)
	settings := FakeApp{App: App{Name: "系统设置", PID: 909}, Elements: []Element{
		{Role: "AXWindow", Title: "通用", Enabled: true},
		{Depth: 1, Role: "AXTextField", Title: "Search", Enabled: true, Actions: []string{"AXConfirm"}},
		{Depth: 1, Role: "AXButton", Title: "Cancel", Enabled: true, Actions: []string{"AXPress"}},
		{Depth: 1, Role: "AXSplitter", Enabled: true, Actions: []string{"AXIncrement"}},
		{Depth: 1, Role: "AXButton", Title: "关于本机", Enabled: true, Actions: []string{"AXPress"}},
	}}
	backend.RunningApps = append(fakeApps(), settings)
	act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"})
	first := act(t, s, "%7", AgentAction{Action: "app_state", App: "系统设置"}).Text
	cancel, about := idOf(t, first, `Button "Cancel"`), idOf(t, first, "关于本机")

	// Acting reads the app again; what didn't change keeps its id.
	again := act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, first, "Search"), ElementAction: "set_value", Text: "显示器"}).Text
	if idOf(t, again, `Button "Cancel"`) != cancel || idOf(t, again, "关于本机") != about {
		t.Fatalf("ids moved:\n%s\n%s", first, again)
	}

	// Cancel goes away and the splitter takes its place in the window.
	backend.mu.Lock()
	app := &backend.RunningApps[len(backend.RunningApps)-1]
	app.Elements = append(app.Elements[:2], app.Elements[3:]...)
	backend.mu.Unlock()
	after := act(t, s, "%7", AgentAction{Action: "app_state", App: "系统设置"}).Text
	if splitter := idOf(t, after, "Splitter"); splitter == cancel || splitter <= about {
		t.Fatalf("the splitter took an old id:\n%s\n%s", first, after)
	}
	pressed := len(backend.ElementActions)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: cancel, ElementAction: "press"}); !errors.Is(err, ErrStaleElement) || !strings.Contains(err.Error(), "isn't in the latest read") {
		t.Fatalf("old id: %v", err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: 999, ElementAction: "press"}); err == nil || !strings.Contains(err.Error(), "no element [999]") {
		t.Fatalf("never given: %v", err)
	}
	if len(backend.ElementActions) != pressed {
		t.Fatal("acted on an old id")
	}

	// Another app's read never reuses ids either.
	notes := act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text
	if id := idOf(t, notes, "New Note"); id <= about {
		t.Fatalf("Notes reused an id:\n%s", notes)
	}
}

// A plain click behind other windows on something the app can press is
// pressed through accessibility; any other is only sent, and said to be.
func TestABackgroundClickPressesWhatItCan(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[1].Hidden = false
	apps[1].Windows = []AppWindow{{ID: 7, Bounds: image.Rect(0, 0, 800, 600)}}
	apps[1].Elements = append(apps[1].Elements, Element{Depth: 1, Role: "AXButton", Title: "Cancel", Frame: image.Rect(180, 180, 240, 220), Enabled: true, Actions: []string{"AXPress"}})
	backend.RunningApps = apps
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})

	got := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 100}, App: "Safari"}).Text
	if !strings.Contains(got, `Pressed Button "Cancel" at that point in Safari`) || !strings.Contains(got, "path: accessibility press") {
		t.Fatalf("pressable: %s", got)
	}
	if len(backend.ElementActions) != 1 || backend.ElementActions[0] != (FakeElementAction{PID: 202, Index: 2, Action: "AXPress"}) || len(backend.AppClicks) != 0 {
		t.Fatalf("pressed %+v, clicked %+v", backend.ElementActions, backend.AppClicks)
	}
	for _, action := range []AgentAction{
		{Action: "left_click", Coordinate: []int{200, 200}, App: "Safari"},
		{Action: "double_click", Coordinate: []int{100, 100}, App: "Safari"},
	} {
		got := act(t, s, "%7", action).Text
		if !strings.Contains(got, "Sent a click to Safari behind other windows (path: SkyLight)") || !strings.Contains(got, "check whether it took effect") || strings.Contains(got, "Clicked") {
			t.Fatalf("%s: %s", action.Action, got)
		}
	}
	if len(backend.AppClicks) != 2 || len(backend.ElementActions) != 1 {
		t.Fatalf("pressed %+v, clicked %+v", backend.ElementActions, backend.AppClicks)
	}
}

// Only the list of windows on the screen runs front to back: a window that
// comes later in the list of all windows can still be over another, as
// Chrome was over System Settings on a real Mac.
func TestCoverersComeFromTheScreensOrder(t *testing.T) {
	settings := windowInfo{PID: 600, Name: "系统设置", ID: 32, OnScreen: true, Bounds: image.Rect(502, 113, 1242, 738)}
	chrome := windowInfo{PID: 700, Name: "Google Chrome", ID: 1173, OnScreen: true, Bounds: image.Rect(0, 33, 1512, 897)}
	all := []windowInfo{
		settings,
		{PID: 600, Name: "系统设置", ID: 33, Bounds: image.Rect(0, 0, 1512, 33)},
		{PID: 700, Name: "Google Chrome", ID: 47, Layer: 103, Bounds: image.Rect(150, 401, 229, 421)},
		chrome,
		{PID: 800, Name: "Music", ID: 900, Bounds: image.Rect(0, 0, 400, 300)},
		{PID: 1, Name: "Mewla", ID: 5, OnScreen: true, Bounds: image.Rect(0, 0, 10, 10)},
	}
	onScreen := []windowInfo{chrome, {PID: 750, Name: "Finder", ID: 3, OnScreen: true, Bounds: image.Rect(1300, 800, 1400, 850)}, settings}
	apps := appsFromWindows(onScreen, all, 700, 1, func(pid int) (string, bool) { return map[int]string{600: "System Settings"}[pid], pid == 700 })
	if len(apps) != 4 {
		t.Fatalf("apps %+v", apps)
	}
	got := apps[2]
	if apps[0].Name != "Google Chrome" || !apps[0].Active || !apps[0].Chromium || got.Name != "系统设置" || got.Hidden || got.Window != settings.Bounds || !slices.Equal(got.CoveredBy, []string{"Google Chrome"}) {
		t.Fatalf("apps %+v", apps)
	}
	if len(got.Windows) != 1 || got.Windows[0].ID != 32 || len(apps[1].CoveredBy) != 1 {
		t.Fatalf("windows %+v, Finder under %v", got.Windows, apps[1].CoveredBy)
	}
	if apps[3].Name != "Music" || !apps[3].Hidden {
		t.Fatalf("an app with no window showing: %+v", apps[3])
	}
	// An app is found, and listed, by its bundle's English name too.
	if app, err := resolveApp(apps, "system settings"); err != nil || app.PID != 600 {
		t.Fatalf("by bundle: %+v %v", app, err)
	}
	if list := formatApps(apps); !strings.Contains(list, "系统设置 (System Settings), pid 600\n") || !strings.Contains(list, "Google Chrome, pid 700, in front\n") {
		t.Fatalf("apps:\n%s", list)
	}
}
