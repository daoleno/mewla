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
	"sync"
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
	// A Mac in Chinese shows Reminders as 提醒事项: it is known by both names,
	// and by its bundle id.
	for _, name := range []string{"reminders", "com.apple.reminders", "COM.APPLE.REMINDERS"} {
		if got := act(t, s, "%7", AgentAction{Action: "open_app", App: name}).Text; got != "提醒事项 is already open (pid 505); read it with app_state." {
			t.Fatalf("by %s: %s", name, got)
		}
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
	if strings.Count(string(audit), `"action":"open_app"`) != 14 {
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

	// Acting reads the app again and answers with what changed; what didn't
	// change keeps its id.
	answer := act(t, s, "%7", AgentAction{Action: "element", Element: idOf(t, first, "Search"), ElementAction: "set_value", Text: "显示器"}).Text
	if strings.Contains(answer, "Cancel") || !strings.Contains(answer, `TextField "Search" = "显示器"`) {
		t.Fatalf("the answer should hold only the change:\n%s", answer)
	}
	again := act(t, s, "%7", AgentAction{Action: "app_state", App: "系统设置"}).Text
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
		if !strings.Contains(got, "Sent a click to Safari behind other windows (path: SkyLight)") || !strings.Contains(got, "No effect seen") || strings.Contains(got, "Clicked") {
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
	apps := appsFromWindows(onScreen, all, 700, 1, func(pid int) bundleInfo {
		return bundleInfo{name: map[int]string{600: "System Settings"}[pid], chromium: pid == 700}
	})
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

// An app already open is found by its bundle id at once, not opened again
// and waited for: Notes is 备忘录 on a Mac in Chinese.
func TestAnOpenAppIsFoundByItsBundleID(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Name, apps[0].Bundle, apps[0].BundleID = "备忘录", "Notes", "com.apple.Notes"
	backend.RunningApps = apps
	start := time.Now()
	if got := act(t, s, "%7", AgentAction{Action: "open_app", App: "com.apple.Notes"}).Text; got != "备忘录 is already open (pid 101); read it with app_state." || time.Since(start) > time.Second {
		t.Fatalf("after %v: %s", time.Since(start), got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "app_state", App: "com.apple.notes"}).Text; !strings.Contains(got, "App: 备忘录, pid 101") {
		t.Fatalf("read by bundle id: %s", got)
	}
	if len(backend.Launched) != 0 {
		t.Fatalf("launched %q", backend.Launched)
	}
}

// An app that quits after it was read is reported as gone, with the way
// back, rather than as an app that didn't answer.
func TestAnAppThatQuitIsSaidToBeGone(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = fakeApps()
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text
	button := idOf(t, state, `Button "New Note"`)
	backend.RunningApps = backend.RunningApps[1:]
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: button, ElementAction: "press"})
	if !errors.Is(err, ErrStaleElement) || !strings.HasSuffix(err.Error(), "Notes (pid 101) quit or closed its windows since it was read; open it with open_app, then read it again") || !strings.HasPrefix(err.Error(), "Not delivered") {
		t.Fatalf("press: %v", err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "app_state", App: "Notes"}); err == nil || !strings.Contains(err.Error(), "open it with open_app") {
		t.Fatalf("read: %v", err)
	}
}

// A combo names a menu shortcut when it has command and one character; the
// modifiers are accessibility's mask, command implied.
func TestMenuShortcuts(t *testing.T) {
	for combo, want := range map[string]Shortcut{
		"cmd+n": {Char: "N"}, "CMD+N": {Char: "N"}, "cmd+shift+n": {Char: "N", Shift: true},
		"cmd+option+f": {Char: "F", Option: true}, "ctrl+cmd+alt+shift+k": {Char: "K", Shift: true, Option: true, Control: true},
		"cmd+,": {Char: ","},
	} {
		if got, ok := menuShortcut(combo); !ok || got != want {
			t.Fatalf("%s: %+v %v", combo, got, ok)
		}
	}
	for _, combo := range []string{"Return", "n", "ctrl+c", "cmd+return", "cmd+a+b", "cmd+space", "cmd", "cmd+é"} {
		if got, ok := menuShortcut(combo); ok {
			t.Fatalf("%s: %+v", combo, got)
		}
	}
	if s := (Shortcut{Char: "F", Option: true}); s.axModifiers() != 2 || !s.matches("f", 2) || s.matches("F", 0) || s.String() != "cmd+option+f" {
		t.Fatalf("mask %d %s", s.axModifiers(), s)
	}
	if s := (Shortcut{Char: "N", Shift: true, Control: true}); s.axModifiers() != 5 {
		t.Fatalf("mask %d", s.axModifiers())
	}
}

// A shortcut sent to an app behind others chooses its menu item, without
// keys and while someone uses the computer; one no item has is sent as keys,
// and the answer says an app behind may ignore them. bring_forward is the
// way out, and waits for the person.
func TestShortcutsChooseMenuItemsBehindOtherWindows(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Active, apps[2].Active = false, true
	apps[0].Menu = []FakeMenuItem{
		{Path: "File > New Note", Char: "N", Adds: []Element{{Depth: 1, Role: "AXTextArea", Enabled: true}}},
		{Path: "Edit > Find > Search", Char: "F", Modifiers: 2},
		{Path: "File > Print…", Char: "P", Disabled: true},
	}
	backend.RunningApps = apps
	act(t, s, "%7", AgentAction{Action: "apps"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()

	if got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+n", App: "Notes"}).Text; got != "Chose File > New Note (cmd+n) in Notes's menus, without sending keys. Read it with app_state to see the result." {
		t.Fatalf("cmd+n: %s", got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+option+f", App: "Notes"}).Text; !strings.Contains(got, "Chose Edit > Find > Search (cmd+option+f)") {
		t.Fatalf("cmd+option+f: %s", got)
	}
	if !strings.Contains(act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text, "TextArea") {
		t.Fatal("the new note isn't there")
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "cmd+p", App: "Notes"}); err == nil || !strings.Contains(err.Error(), "File > Print… (cmd+p) is turned off") {
		t.Fatalf("turned off: %v", err)
	}
	if len(backend.AppKeys) != 0 || !slices.Equal(backend.MenuPresses, []string{"File > New Note", "Edit > Find > Search"}) {
		t.Fatalf("keys %v, menus %v", backend.AppKeys, backend.MenuPresses)
	}
	// No item has cmd+shift+n: the keys go to the app, which may ignore them.
	if got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+shift+n", App: "Notes"}).Text; !strings.Contains(got, "Sent it to Notes behind other windows. No effect seen") || len(backend.AppKeys) == 0 {
		t.Fatalf("not a menu shortcut: %s", got)
	}
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInputOf(InputPointer)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "bring_forward", App: "Notes"}); !errors.Is(err, ErrPersonAtComputer) || len(backend.Raised) != 0 {
		t.Fatalf("bring_forward while someone types: %v %v", err, backend.Raised)
	}
}

// bring_forward brings an app to the front once nobody is using the
// computer, and says so when it already is.
func TestBringForward(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = fakeApps()
	if got := act(t, s, "%7", AgentAction{Action: "bring_forward", App: "Safari"}).Text; !strings.Contains(got, "Brought Safari to the front") || !slices.Equal(backend.Raised, []int{202}) {
		t.Fatalf("%s %v", got, backend.Raised)
	}
	if got := act(t, s, "%7", AgentAction{Action: "bring_forward", App: "Safari"}).Text; got != "Safari is already in front." {
		t.Fatalf("again: %s", got)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "bring_forward"}); err == nil {
		t.Fatal("no app")
	}
}

// read gives an element's whole text, lines and all, which a read of the
// app cuts short; up to valueRead characters, and never a password.
func TestReadAnElementsWholeValue(t *testing.T) {
	s, backend, dir := newAgentService(t)
	apps := fakeApps()
	body := "Mewla check\nNative computer benchmark\n" + strings.Repeat("a long line of the note ", 40)
	apps[0].Elements = append(apps[0].Elements, Element{Depth: 1, Role: "AXTextArea", Value: body, Enabled: true},
		Element{Depth: 1, Role: "AXTextArea", Title: "Huge", Value: strings.Repeat("x", valueRead+5), Enabled: true})
	backend.RunningApps = apps
	state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text
	if strings.Contains(state, "Native computer benchmark\n") || !strings.Contains(state, "…") {
		t.Fatalf("the read already holds it all:\n%s", state)
	}
	note := idOf(t, state, "TextArea = \"Mewla check")
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: note, ElementAction: "read"}).Text; got != fmt.Sprintf("[%d] TextArea holds %d characters:\n%s", note, len(body), body) {
		t.Fatalf("read:\n%s", got)
	}
	huge := idOf(t, state, `TextArea "Huge"`)
	if got := act(t, s, "%7", AgentAction{Action: "element", Element: huge, ElementAction: "read"}).Text; !strings.HasPrefix(got, fmt.Sprintf("[%d] TextArea \"Huge\" holds %d characters; the first %d:\n", huge, valueRead+5, valueRead)) || !strings.HasSuffix(got, ":\n"+strings.Repeat("x", valueRead)) {
		t.Fatalf("huge: %.120s", got)
	}
	password := idOf(t, state, "SecureTextField")
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: password, ElementAction: "read"}); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("password: %v", err)
	}
	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if strings.Contains(string(audit), "Native computer") || !strings.Contains(string(audit), `"element_action":"read"`) {
		t.Fatalf("audit %s", audit)
	}
}

// An app that takes the front when its menu item is chosen behind others,
// as Notes does for New Note on a Mac, gives it back to the person's app at
// once, and the answer says so.
func TestChoosingAMenuItemGivesTheFrontBack(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Active, apps[2].Active = false, true // Helper is the person's
	apps[0].Menu = []FakeMenuItem{
		{Path: "File > New Note", Char: "N", Activates: true, Adds: []Element{{Depth: 1, Role: "AXTextArea", Enabled: true}}},
		{Path: "Format > Bold", Char: "B"},
	}
	backend.RunningApps = apps
	front := func() int { pid, _ := backend.FrontPID(context.Background()); return pid }
	if before := front(); before != 301 {
		t.Fatalf("in front before: %d", before)
	}
	got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+n", App: "Notes"}).Text
	if got != "Chose File > New Note (cmd+n) in Notes's menus, without sending keys. Choosing it brought Notes to the front; the front was given back to Helper. Read it with app_state to see the result." {
		t.Fatalf("cmd+n: %s", got)
	}
	if after := front(); after != 301 || !slices.Equal(backend.Raised, []int{301}) {
		t.Fatalf("in front after: %d, raised %v", after, backend.Raised)
	}
	// An item that leaves the front alone says nothing of it.
	if got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+b", App: "Notes"}).Text; strings.Contains(got, "front") || len(backend.Raised) != 1 || front() != 301 {
		t.Fatalf("cmd+b: %s %v", got, backend.Raised)
	}
}

// Behind other windows, an item that is turned off says what may turn it on.
func TestATurnedOffMenuItemSaysWhy(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Menu = []FakeMenuItem{{Path: "Edit > Find > Note List Search…", Char: "F", Modifiers: 2, Disabled: true}}
	backend.RunningApps = apps
	// In front, it is off for the app's own reasons.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "cmd+option+f", App: "Notes"}); err == nil || strings.Contains(err.Error(), "bring_forward") {
		t.Fatalf("in front: %v", err)
	}
	backend.RunningApps[0].Active, backend.RunningApps[2].Active = false, true
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "cmd+option+f", App: "Notes"})
	if err == nil || !strings.Contains(err.Error(), "Edit > Find > Note List Search… (cmd+option+f) is turned off") || !strings.Contains(err.Error(), "often off while the app isn't in front: bring_forward it when nobody is using the computer") {
		t.Fatalf("behind: %v", err)
	}
}

// An app whose windows are on another desktop reads as such, not as an app
// that shows little to accessibility; once in front it reads in full.
func TestAnAppOnAnotherDesktopSaysSo(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Active, apps[2].Active = false, true
	apps[0].OtherDesktop = true
	backend.RunningApps = apps
	got := act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text
	if !strings.Contains(got, "none of its windows is on this screen (it may be on another desktop, or hidden)") || strings.Contains(got, "shows little") {
		t.Fatalf("behind:\n%s", got)
	}
	act(t, s, "%7", AgentAction{Action: "bring_forward", App: "Notes"})
	if got := act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text; !strings.Contains(got, `Button "New Note"`) {
		t.Fatalf("in front:\n%s", got)
	}
	if got := act(t, s, "%7", AgentAction{Action: "app_state", App: "301"}).Text; !strings.Contains(got, "shows little to accessibility") {
		t.Fatalf("an app with nothing to read:\n%s", got)
	}
}

// A native password field is an AXTextField whose subrole is
// AXSecureTextField: its value is hidden, can't be matched and can't be read,
// as for the role of the same name.
func TestAPasswordFieldBySubroleNeverShowsItsValue(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = []FakeApp{{App: App{Name: "Bank", PID: 505, Active: true}, Elements: []Element{
		{Role: "AXWindow", Title: "Sign in", Enabled: true},
		{Depth: 1, Role: "AXTextField", Title: "User", Value: "ada", Enabled: true, Actions: []string{"AXConfirm"}},
		{Depth: 1, Role: "AXTextField", Subrole: "AXSecureTextField", Title: "Password", Value: "hunter2", Enabled: true, Actions: []string{"AXConfirm"}},
		{Depth: 1, Role: "AXButton", Title: "Go", Enabled: true, Actions: []string{"AXPress"}},
	}}}
	state := act(t, s, "%7", AgentAction{Action: "app_state"}).Text
	if strings.Contains(state, "hunter2") || !strings.Contains(state, `TextField "Password"`) || !strings.Contains(state, `= "ada"`) {
		t.Fatalf("app_state:\n%s", state)
	}
	password := idOf(t, state, `"Password"`)

	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "element", Element: password, ElementAction: "read"}); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("read a password: %v", err)
	}
	for _, m := range []ElementMatch{{Value: "hunter2"}, {Role: "TextField", Title: "Password", Empty: true}} {
		if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "expect", Match: &m}); err == nil {
			t.Fatalf("matched a password's value: %+v", m)
		}
	}
	set := act(t, s, "%7", AgentAction{Action: "element", Element: password, ElementAction: "set_value", Text: "newsecret"}).Text
	if strings.Contains(set, "newsecret") || strings.Contains(set, "hunter2") {
		t.Fatalf("the answer shows a password:\n%s", set)
	}
}

// A click or a key to an app behind others says what the app's focus shows
// afterwards: a change, no change, or that the app can't be read. It never
// says only that the event was sent.
func TestAnActionBehindOtherWindowsSaysWhetherItShowed(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[1].Hidden = false
	apps[1].Windows = []AppWindow{{ID: 7, Bounds: image.Rect(0, 0, 800, 600)}}
	apps[1].Elements = append(apps[1].Elements, Element{Depth: 1, Role: "AXTextArea", Title: "Body", Frame: image.Rect(100, 100, 700, 500), Enabled: true, Focused: true})
	backend.RunningApps = apps
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	last := len(apps[1].Elements) - 1

	click := AgentAction{Action: "left_click", Coordinate: []int{200, 200}, App: "Safari"}
	if got := act(t, s, "%7", click).Text; !strings.Contains(got, "No effect seen") || strings.Contains(got, "Effect seen") {
		t.Fatalf("a click the app ignored: %s", got)
	}

	backend.OnAppClick = func(app *FakeApp, _ FakeAppClick) { app.Elements[last].Value = "caret moved" }
	if got := act(t, s, "%7", click).Text; !strings.Contains(got, "Effect seen: Safari changed the focused field's text") {
		t.Fatalf("a click the app took: %s", got)
	}

	backend.OnAppKey = func(app *FakeApp, k FakeKey) {
		if k.Down {
			app.Elements[last].Value += "x"
		}
	}
	if got := act(t, s, "%7", AgentAction{Action: "type", Text: "ab", App: "Safari"}).Text; !strings.Contains(got, "Sent it to Safari behind other windows. Effect seen") {
		t.Fatalf("keys the app took: %s", got)
	}

	backend.NoWitness = true
	if got := act(t, s, "%7", click).Text; !strings.Contains(got, "Effect not checked") {
		t.Fatalf("an app that can't be read: %s", got)
	}
}

func parkingApps() []FakeApp {
	apps := fakeApps()
	apps[1].Hidden = false
	apps[1].Windows = []AppWindow{{ID: 7, Bounds: image.Rect(100, 100, 900, 700)}}
	apps[1].Window = apps[1].Windows[0].Bounds
	apps[1].Elements = append(apps[1].Elements, Element{Depth: 1, Role: "AXTextArea", Title: "Body", Focused: true, Frame: image.Rect(150, 150, 850, 650), Enabled: true})
	apps[0].Windows = []AppWindow{{ID: 5, Bounds: image.Rect(0, 0, 2000, 1400)}}
	apps[0].Window = apps[0].Windows[0].Bounds
	return apps
}

// An app an agent acts on while it isn't in front is moved to the agent
// display, is clicked there by points on its own window screenshot, and goes
// back when the agent is done. The person's app in front is never moved.
func TestAnAppTheAgentActsOnWaitsOnTheAgentDisplay(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	last := len(backend.RunningApps[1].Elements) - 1
	backend.OnAppClick = func(app *FakeApp, _ FakeAppClick) { app.Elements[last].Value = "x" }

	// Reading doesn't move it.
	act(t, s, "%7", AgentAction{Action: "app_state", App: "Safari"})
	if backend.ParkCalls != 0 {
		t.Fatal("reading moved the app")
	}

	// The first click does, and says so.
	got := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"}).Text
	if !strings.HasPrefix(got, "Safari is on the agent display, off your screen") || !strings.Contains(got, "Effect seen") {
		t.Fatalf("first click: %s", got)
	}
	if backend.ParkCalls != 1 || !backend.Parked[202] {
		t.Fatalf("parked %v after %d moves", backend.Parked, backend.ParkCalls)
	}
	// The click went to the window by its own point, not by the screenshot's.
	if len(backend.AppClicks) != 1 {
		t.Fatalf("clicks %+v", backend.AppClicks)
	}
	c := backend.AppClicks[0]
	scale := 2.0 // the fake screen is 2000 wide: a screenshot pixel is two points
	want := image.Pt(fakeDisplayShift+100+int(100*scale), 100+int(80*scale))
	if c.Window != 7 || image.Pt(c.X, c.Y) != want {
		t.Fatalf("click at %+v, want window 7 at %v", c, want)
	}

	// A second action doesn't move it again, and says nothing of it.
	got = act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"}).Text
	if strings.Contains(got, "agent display") || backend.ParkCalls != 1 {
		t.Fatalf("second click: %s (%d moves)", got, backend.ParkCalls)
	}

	// A point outside its window screenshot is refused, not sent.
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{5000, 5000}, App: "Safari"}); err == nil || !strings.Contains(err.Error(), "outside Safari's window screenshot") {
		t.Fatalf("outside: %v", err)
	}
	if len(backend.AppClicks) != 2 {
		t.Fatalf("an outside click was sent: %+v", backend.AppClicks)
	}

	// Its window screenshot comes from the agent display.
	shot := act(t, s, "%7", AgentAction{Action: "screenshot", App: "Safari"})
	if backend.ParkedShots != 1 || !strings.Contains(shot.Text, "on the agent display") || shot.Width != 400 || shot.Height != 300 {
		t.Fatalf("window shot %dx%d: %s (%d from the display)", shot.Width, shot.Height, shot.Text, backend.ParkedShots)
	}

	// The person's app in front is never parked.
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Notes"})
	if backend.Parked[101] {
		t.Fatal("the app in front was parked")
	}

	// bring_forward puts it back before raising it.
	act(t, s, "%7", AgentAction{Action: "bring_forward", App: "Safari"})
	if backend.Parked[202] || len(backend.Restored) != 1 || backend.Restored[0] != 202 || len(backend.Raised) != 1 {
		t.Fatalf("parked %v restored %v raised %v", backend.Parked, backend.Restored, backend.Raised)
	}
}

func TestParkedAppsGoBackWhenTheAgentIsDone(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	if !backend.Parked[202] {
		t.Fatal("not parked")
	}
	s.StopAgent()
	deadline := time.Now().Add(3 * time.Second)
	for {
		backend.mu.Lock()
		back := len(backend.Restored) == 1 && !backend.Parked[202]
		backend.mu.Unlock()
		if back {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the app wasn't put back: parked %v restored %v", backend.Parked, backend.Restored)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if w := backend.RunningApps[1].Windows[0].Bounds; w != image.Rect(100, 100, 900, 700) {
		t.Fatalf("its window is at %v", w)
	}
}

// Without an agent display the agent works behind the person's windows, and is
// told once.
func TestWithoutAnAgentDisplayTheAppStaysAndTheAgentIsToldOnce(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	backend.NoAgentDisplay = "this macOS can't make a display"
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	first := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"}).Text
	if !strings.HasPrefix(first, "No agent display here (this macOS can't make a display), so Safari stays behind your windows.") {
		t.Fatalf("first: %s", first)
	}
	second := act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"}).Text
	if strings.Contains(second, "No agent display") {
		t.Fatalf("told twice: %s", second)
	}
	if backend.Parked[202] {
		t.Fatal("parked without a display")
	}
	// The click went where the screenshot's point is.
	c := backend.AppClicks[0]
	if image.Pt(c.X, c.Y) != image.Pt(200, 160) {
		t.Fatalf("click at %+v", c)
	}
}

// A display that went away (the Mac slept) takes the windows back; the next
// action moves them again and says so.
func TestParkedAppsAreMovedAgainAfterTheDisplayWentAway(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	backend.mu.Lock()
	for j := range backend.RunningApps[1].Windows {
		backend.RunningApps[1].Windows[j].Bounds = backend.RunningApps[1].Windows[j].Bounds.Sub(image.Pt(fakeDisplayShift, 0))
	}
	backend.RunningApps[1].Window = backend.RunningApps[1].Windows[0].Bounds
	backend.mu.Unlock()
	got := act(t, s, "%7", AgentAction{Action: "key", Text: "b", App: "Safari"}).Text
	if !strings.Contains(got, "was back on your screen") || !strings.Contains(got, "on the agent display again") {
		t.Fatalf("after the wake: %s", got)
	}
	if !backend.ParkedOnDisplay(202) {
		t.Fatal("not moved again")
	}
}

// Cmd+w is off while the app is behind other windows; the window's own close
// button closes it from there, without bringing anything forward.
func TestCloseWindowFallsBackToTheCloseButtonBehindOtherWindows(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Menu = []FakeMenuItem{{Path: "File > Close", Char: "W", Disabled: true}}
	apps[0].Elements = []Element{
		{Role: "AXWindow", Title: "Untitled", Enabled: true},
		{Depth: 1, Role: "AXButton", Subrole: "AXCloseButton", Enabled: true, Actions: []string{"AXPress"}},
		{Depth: 1, Role: "AXTextArea", Value: "alpha one", Enabled: true},
	}
	apps[0].Active, apps[2].Active = false, true
	backend.RunningApps = apps
	got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+w", App: "Notes"}).Text
	if !strings.Contains(got, `pressed the close button of its key window "Untitled"`) || strings.Contains(got, "unsaved") {
		t.Fatalf("close:\n%s", got)
	}
	if !slices.Equal(backend.ClosePresses, []int{backend.RunningApps[0].PID}) || len(backend.Raised) != 0 {
		t.Fatalf("close presses %v, raised %v", backend.ClosePresses, backend.Raised)
	}
	if front, _ := backend.FrontPID(context.Background()); front != 301 {
		t.Fatalf("the front changed to %d", front)
	}
	if state := act(t, s, "%7", AgentAction{Action: "app_state", App: "Notes"}).Text; strings.Contains(state, "alpha one") {
		t.Fatalf("the window is still there:\n%s", state)
	}
}

// A document with unsaved changes puts up a sheet instead: nothing is chosen
// for the Worker; it reads the sheet and presses what its task says.
func TestCloseWindowWithUnsavedChangesLeavesTheChoiceToTheWorker(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Menu = []FakeMenuItem{{Path: "File > Close", Char: "W", Disabled: true}}
	apps[0].Elements = []Element{
		{Role: "AXWindow", Title: "Untitled", Enabled: true},
		{Depth: 1, Role: "AXButton", Subrole: "AXCloseButton", Enabled: true, Actions: []string{"AXPress"}},
	}
	apps[0].SaveSheet = []string{"Delete", "Cancel", "Save"}
	apps[0].Active, apps[2].Active = false, true
	backend.RunningApps = apps
	got := act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+w", App: "Notes"}).Text
	for _, want := range []string{`"Delete", "Cancel", "Save"`, "Nothing was chosen", "only discard the document if your task said to"} {
		if !strings.Contains(got, want) {
			t.Errorf("answer lacks %q:\n%s", want, got)
		}
	}
}

// Only the plain Close is replaced; other items that are off still say so,
// and an app without a key window is not guessed at.
func TestCloseFallbackIsOnlyForCmdW(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[0].Menu = []FakeMenuItem{{Path: "File > Close All", Char: "W", Modifiers: 2, Disabled: true}, {Path: "File > Close", Char: "W", Disabled: true}}
	apps[0].Active, apps[2].Active = false, true
	backend.RunningApps = apps
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "cmd+option+w", App: "Notes"}); err == nil || len(backend.ClosePresses) != 0 {
		t.Fatalf("cmd+option+w: %v %v", err, backend.ClosePresses)
	}
	backend.RunningApps[0].Elements = nil
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "cmd+w", App: "Notes"})
	if err == nil || !strings.Contains(err.Error(), "no key window to close") || !strings.Contains(err.Error(), "bring_forward") {
		t.Fatalf("no window: %v", err)
	}
}

func parkedAfterIdle(t *testing.T) (*Service, *FakeBackend) {
	t.Helper()
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.parkLease = time.Hour
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	if !backend.Parked[202] {
		t.Fatal("not parked")
	}
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	return s, backend
}

// An agent that only went quiet is thinking, not done: its app stays on the
// agent display, still its own, and its next action finds it there.
func TestAQuietAgentKeepsItsAppOnTheAgentDisplay(t *testing.T) {
	s, backend := parkedAfterIdle(t)
	time.Sleep(200 * time.Millisecond)
	backend.mu.Lock()
	still, restored := backend.Parked[202], len(backend.Restored)
	backend.mu.Unlock()
	if !still || restored != 0 {
		t.Fatalf("the app went back: parked %v restored %d", still, restored)
	}
	// Another agent is told the app is taken.
	if _, err := s.Act(context.Background(), "%8", AgentAction{Action: "key", Text: "b", App: "Safari"}); err == nil || !strings.Contains(err.Error(), "Another agent") {
		t.Fatalf("a second agent: %v", err)
	}
	// Its own next action neither moves it again nor mentions it.
	moves := backend.ParkCalls
	got := act(t, s, "%7", AgentAction{Action: "key", Text: "c", App: "Safari"}).Text
	if strings.Contains(got, "agent display") || backend.ParkCalls != moves {
		t.Fatalf("next action: %s (%d moves, was %d)", got, backend.ParkCalls, moves)
	}
}

// Stopping the agent from the app, or a person taking over, puts it back at
// once, even when the agent is quiet.
func TestAQuietAgentsAppGoesBackWhenItIsStoppedOrTheScreenIsTaken(t *testing.T) {
	for name, end := range map[string]func(*Service){
		"stopped": func(s *Service) { s.StopAgentFor("%7") },
		"taken":   func(s *Service) { s.TakeControl(func(error) {}) },
	} {
		t.Run(name, func(t *testing.T) {
			s, backend := parkedAfterIdle(t)
			end(s)
			waitFor(t, func() bool {
				backend.mu.Lock()
				defer backend.mu.Unlock()
				return !backend.Parked[202] && len(backend.Restored) == 1
			})
			// The app is free for another agent again.
			s.agentMu.Lock()
			_, owned := s.appOwners[202]
			s.agentMu.Unlock()
			if owned {
				t.Fatal("the app is still claimed")
			}
		})
	}
}

// An agent that never comes back does not keep the app away for ever.
func TestAQuietAgentsAppGoesBackWhenTheLeaseEnds(t *testing.T) {
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.parkLease = 300 * time.Millisecond
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	waitFor(t, func() bool {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		return !backend.Parked[202] && len(backend.Restored) == 1
	})
}

// A Worker that is closed, or whose Work ends, gives back what it holds at
// once: the lease is only the fallback.
func TestAnEndedWorkersAppsGoBackAtOnce(t *testing.T) {
	var mu sync.Mutex
	ended := map[string]bool{}
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.aliveEvery = 30 * time.Millisecond
	s.parkLease = time.Hour
	s.SetWorkerAlive(func(w string) bool { mu.Lock(); defer mu.Unlock(); return !ended[w] })
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	// Alive and quiet: it stays.
	time.Sleep(200 * time.Millisecond)
	backend.mu.Lock()
	stays := backend.Parked[202]
	backend.mu.Unlock()
	if !stays {
		t.Fatal("the app went back while its Worker lives")
	}
	mu.Lock()
	ended["%7"] = true
	mu.Unlock()
	waitFor(t, func() bool {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		return !backend.Parked[202] && len(backend.Restored) == 1
	})
	s.agentMu.Lock()
	_, owned := s.appOwners[202]
	s.agentMu.Unlock()
	if owned {
		t.Fatal("the app is still claimed")
	}
}

// A Worker that ends while it is acting loses the screen and its apps go back.
func TestAWorkerThatEndsWhileActingLosesTheScreen(t *testing.T) {
	var mu sync.Mutex
	ended := false
	s, backend, _ := newAgentService(t)
	s.aliveEvery = 20 * time.Millisecond
	s.SetWorkerAlive(func(string) bool { mu.Lock(); defer mu.Unlock(); return !ended })
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	mu.Lock()
	ended = true
	mu.Unlock()
	waitFor(t, func() bool {
		backend.mu.Lock()
		defer backend.mu.Unlock()
		return !backend.Parked[202] && len(backend.Restored) == 1
	})
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
}

// One look that misses the Worker (a daemon that has just started and not
// found its Workers yet) does not end anything.
func TestOneMissedLookDoesNotEndTheWorker(t *testing.T) {
	var mu sync.Mutex
	asks := 0
	s, backend, _ := newAgentService(t)
	s.aliveEvery = 20 * time.Millisecond
	s.SetWorkerAlive(func(string) bool { mu.Lock(); defer mu.Unlock(); asks++; return asks != 2 })
	backend.RunningApps = parkingApps()
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(400 * time.Millisecond)
	if _, ok := s.Agent(); !ok {
		t.Fatal("one missed look ended the agent")
	}
}

// The display stays on for an agent through its quiet spell: from its first
// action until its Worker ends, and ten minutes after its last action at
// most; Stop, a person taking over and the Worker ending let go at once.
func TestTheDisplayStaysOnForAQuietAgentUntilItsWorkerEnds(t *testing.T) {
	var mu sync.Mutex
	ended := false
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.aliveEvery = 20 * time.Millisecond
	s.parkLease = time.Hour
	s.SetWorkerAlive(func(string) bool { mu.Lock(); defer mu.Unlock(); return !ended })
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	waitHeld(t, backend, true)
	if !s.DisplayKeptForAgents() {
		t.Fatal("not reported while the agent works")
	}
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	time.Sleep(200 * time.Millisecond)
	if !displayHeld(backend) || !s.DisplayKeptForAgents() {
		t.Fatal("the display was let go while the agent was only quiet")
	}
	// Its next action takes the hold over: still one hold, and still on.
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	waitFor(t, func() bool { return s.DisplayKeptForAgents() })
	mu.Lock()
	ended = true
	mu.Unlock()
	waitHeld(t, backend, false)
	if s.DisplayKeptForAgents() {
		t.Fatal("still reported after the Worker ended")
	}
}

func TestTheDisplayIsLetGoAtOnceWhenTheAgentIsStopped(t *testing.T) {
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.parkLease = time.Hour
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	if !displayHeld(backend) {
		t.Fatal("not held through the quiet spell")
	}
	s.StopAgentFor("%7")
	waitHeld(t, backend, false)
}

func TestTheDisplayIsLetGoWhenAPersonTakesOver(t *testing.T) {
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.parkLease = time.Hour
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	s.TakeControl(func(error) {})
	waitHeld(t, backend, false)
}

func TestTheDisplayHoldEndsTenMinutesAfterTheLastActionAtMost(t *testing.T) {
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.parkLease = 400 * time.Millisecond
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	waitHeld(t, backend, false)
}

type turnWorld struct {
	mu     sync.Mutex
	turn   bool
	closed bool
}

func (w *turnWorld) set(turn, closed bool) {
	w.mu.Lock()
	w.turn, w.closed = turn, closed
	w.mu.Unlock()
}

func turnService(t *testing.T) (*Service, *FakeBackend, *turnWorld) {
	t.Helper()
	world := &turnWorld{turn: true}
	s, backend, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	s.aliveEvery = 20 * time.Millisecond
	s.parkLease = 400 * time.Millisecond // the ten minutes after a turn ends
	s.SetWorkerAlive(func(string) bool { world.mu.Lock(); defer world.mu.Unlock(); return !world.closed })
	s.SetWorkerTurnActive(func(string) bool { world.mu.Lock(); defer world.mu.Unlock(); return world.turn && !world.closed })
	backend.RunningApps = parkingApps()
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "a", App: "Safari"})
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	return s, backend, world
}

func appBack(b *FakeBackend) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.Parked[202] && len(b.Restored) == 1
}

// While the Worker's turn is running the display and the app stay held, however
// long the agent is quiet (several times the ten minutes, here the lease).
func TestARunningTurnKeepsTheDisplayAndAppNoMatterHowQuiet(t *testing.T) {
	s, backend, _ := turnService(t)
	time.Sleep(1600 * time.Millisecond) // four leases
	if !displayHeld(backend) || !s.DisplayKeptForAgents() || appBack(backend) || !backend.ParkedOnDisplay(202) {
		t.Fatalf("let go while the turn ran: display %v, parked %v", displayHeld(backend), backend.Parked)
	}
}

// When the turn ends the hold goes after the lease, not before and not never.
func TestTheHoldEndsALeaseAfterTheTurnEnds(t *testing.T) {
	s, backend, world := turnService(t)
	world.set(false, false)
	time.Sleep(200 * time.Millisecond)
	if !displayHeld(backend) {
		t.Fatal("let go before the lease after the turn ended")
	}
	waitHeld(t, backend, false)
	waitFor(t, func() bool { return appBack(backend) })
	if s.DisplayKeptForAgents() {
		t.Fatal("still reported")
	}
}

// Closing the Worker lets go at once, whatever its turn was doing.
func TestClosingTheWorkerReleasesAtOnce(t *testing.T) {
	_, backend, world := turnService(t)
	world.set(true, true)
	start := time.Now()
	waitHeld(t, backend, false)
	if time.Since(start) > 300*time.Millisecond {
		t.Fatalf("took %v", time.Since(start))
	}
	waitFor(t, func() bool { return appBack(backend) })
}

// A new turn before the lease is up starts the count again.
func TestANewTurnReArmsTheHold(t *testing.T) {
	s, backend, world := turnService(t)
	world.set(false, false)
	time.Sleep(250 * time.Millisecond) // most of the lease
	world.set(true, false)
	time.Sleep(900 * time.Millisecond) // more than two leases, turn running
	if !displayHeld(backend) || !s.DisplayKeptForAgents() {
		t.Fatal("a new turn did not keep the hold")
	}
	world.set(false, false)
	time.Sleep(200 * time.Millisecond)
	if !displayHeld(backend) {
		t.Fatal("the count did not start again")
	}
	waitHeld(t, backend, false)
}
