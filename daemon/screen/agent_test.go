package screen

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A Retina-sized display: screenshots are half its size.
func newAgentService(t *testing.T) (*Service, *FakeBackend, string) {
	t.Helper()
	dir := t.TempDir()
	backend := NewFakeBackend(2560, 1600)
	s := NewServiceWith(dir, backend)
	t.Cleanup(func() {
		s.StopAgent()
		waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	})
	return s, backend, dir
}

func act(t *testing.T, s *Service, worker string, action AgentAction) AgentResult {
	t.Helper()
	result, err := s.Act(context.Background(), worker, action)
	if err != nil {
		t.Fatalf("%s: %v", action.Action, err)
	}
	return result
}

func TestAgentSeesAScaledScreenAndClicksWhereItPoints(t *testing.T) {
	s, backend, _ := newAgentService(t)
	shot := act(t, s, "%7", AgentAction{Action: "screenshot"})
	if shot.Width != 1280 || shot.Height != 800 {
		t.Fatalf("screenshot %dx%d", shot.Width, shot.Height)
	}
	img, err := jpeg.Decode(bytes.NewReader(shot.Image))
	if err != nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 800 {
		t.Fatalf("jpeg %v %v", img.Bounds(), err)
	}

	act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{640, 400}})
	pointers, _ := backend.Snapshot()
	last := pointers[len(pointers)-2:]
	if last[0] != (FakePointer{1280, 800, 1}) || last[1] != (FakePointer{1280, 800, 0}) {
		t.Fatalf("click reached %v", last)
	}
	if got := act(t, s, "%7", AgentAction{Action: "cursor_position"}).Text; got != "X=640,Y=400" {
		t.Fatalf("cursor %q", got)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{1281, 10}}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("off-screen click: %v", err)
	}

	act(t, s, "%7", AgentAction{Action: "left_click_drag", StartCoordinate: []int{10, 10}, Coordinate: []int{110, 60}})
	pointers, _ = backend.Snapshot()
	if end := pointers[len(pointers)-1]; end != (FakePointer{220, 120, 0}) {
		t.Fatalf("drag ended at %v", end)
	}
	act(t, s, "%7", AgentAction{Action: "scroll", Coordinate: []int{100, 100}, ScrollDirection: "down", ScrollAmount: 2})
	pointers, _ = backend.Snapshot()
	downs := 0
	for _, p := range pointers[len(pointers)-4:] {
		if p.Buttons == 16 {
			downs++
		}
	}
	if downs != 2 {
		t.Fatalf("scroll: %v", pointers[len(pointers)-4:])
	}
}

func TestAgentTypesAnyTextAndPressesCombos(t *testing.T) {
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "type", Text: "Hi 收到\n"})
	_, keys := backend.Snapshot()
	want := []uint32{'H', 'i', ' ', 0x01000000 + '收', 0x01000000 + '到', 0xff0d}
	var downs []uint32
	for _, k := range keys {
		if k.Down {
			downs = append(downs, k.Keysym)
		}
	}
	if len(downs) != len(want) {
		t.Fatalf("typed %x", downs)
	}
	for i := range want {
		if downs[i] != want[i] {
			t.Fatalf("typed %x, want %x", downs, want)
		}
	}

	act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+Shift+4"})
	_, keys = backend.Snapshot()
	combo := keys[len(keys)-6:]
	expect := []FakeKey{{0xffeb, true}, {0xffe1, true}, {'4', true}, {'4', false}, {0xffe1, false}, {0xffeb, false}}
	for i := range expect {
		if combo[i] != expect[i] {
			t.Fatalf("combo %v", combo)
		}
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "cmd+hyper"}); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestAPersonComesFirst(t *testing.T) {
	s, _, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	if state, ok := s.Agent(); !ok || state.Worker != "%7" {
		t.Fatalf("agent state %+v %v", state, ok)
	}
	if _, err := s.Act(context.Background(), "%8", AgentAction{Action: "screenshot"}); err == nil || !strings.Contains(err.Error(), "%7") {
		t.Fatalf("second agent: %v", err)
	}

	// A viewer connects: the agent's control ends at once.
	ctx, cancel := context.WithCancelCause(context.Background())
	person := s.TakeControl(cancel)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrAgentStopped) && !errors.Is(err, ErrPersonControlling) {
		t.Fatalf("agent kept the screen from a person: %v", err)
	}
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	if _, err := s.Act(context.Background(), "%8", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrPersonControlling) {
		t.Fatalf("agent took the screen from a person: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("the agent's attempt ended the person's control")
	}
	s.ReleaseControl(person)
	act(t, s, "%8", AgentAction{Action: "screenshot"})

	// An agent never ends a person's viewer the other way round either:
	// a person who connects later still replaces the agent.
	s.StopAgent()
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
}

// A person who takes over mid-action gets the screen at once, with nothing
// left pressed.
func TestTakingOverInterruptsAnActionAndLetsGo(t *testing.T) {
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "left_mouse_down", Coordinate: []int{10, 10}})
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := s.Act(context.Background(), "%7", AgentAction{Action: "hold_key", Text: "shift+a", Duration: 5})
		done <- err
	}()
	waitFor(t, func() bool {
		_, keys := backend.Snapshot()
		return len(keys) == 2
	})
	_, cancel := context.WithCancelCause(context.Background())
	person := s.TakeControl(cancel)
	defer s.ReleaseControl(person)
	if err := <-done; !errors.Is(err, ErrAgentStopped) || time.Since(start) > 2*time.Second {
		t.Fatalf("held on for %v: %v", time.Since(start), err)
	}
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	pointers, keys := backend.Snapshot()
	want := []FakeKey{{0xffe1, true}, {'a', true}, {'a', false}, {0xffe1, false}}
	if len(keys) != len(want) {
		t.Fatalf("keys %v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys %v", keys)
		}
	}
	if last := pointers[len(pointers)-1]; last.Buttons != 0 {
		t.Fatalf("button left pressed: %v", pointers)
	}
}

// However the agent's control ends, a button it left down comes up.
func TestAnEndedAgentLeavesNoButtonDown(t *testing.T) {
	for name, end := range map[string]func(*Service){
		"take over": func(s *Service) {
			_, cancel := context.WithCancelCause(context.Background())
			s.ReleaseControl(s.TakeControl(cancel))
		},
		"stop": func(s *Service) { s.StopAgent() },
	} {
		t.Run(name, func(t *testing.T) {
			s, backend, _ := newAgentService(t)
			act(t, s, "%7", AgentAction{Action: "left_mouse_down", Coordinate: []int{10, 10}})
			end(s)
			waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
			waitFor(t, func() bool {
				pointers, _ := backend.Snapshot()
				return pointers[len(pointers)-1] == FakePointer{20, 20, 0}
			})
		})
	}
}

func TestTurningScreenOrAgentsOffStopsTheAgent(t *testing.T) {
	s, _, dir := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	if err := s.Disable(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })

	// Turned off by another process: the agent sees it within seconds.
	if err := SaveConfig(dir, Config{}); err != nil {
		t.Fatal(err)
	}
	s.agentIdle = 4 * time.Second // looks every second
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	if err := SaveConfig(dir, Config{Disabled: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })

	if err := SaveConfig(dir, Config{}); err != nil {
		t.Fatal(err)
	}
	var on atomic.Bool
	on.Store(true)
	s.SetAgentGate(on.Load)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	on.Store(false)
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
}

func TestAgentsNeedScreenToBeOnAndAllowed(t *testing.T) {
	s, backend, dir := newAgentService(t)
	backend.Missing = []string{"accessibility"}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrPermissions) {
		t.Fatalf("missing permission: %v", err)
	}
	backend.Missing = nil
	if err := SaveConfig(dir, Config{Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Screen turned off: %v", err)
	}
	backend.Unsupported = true
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
	if _, err := s.Act(context.Background(), "", AgentAction{Action: "screenshot"}); err == nil {
		t.Fatal("acted for no Worker")
	}
}

func TestAnIdleAgentLetsGo(t *testing.T) {
	s, _, _ := newAgentService(t)
	s.agentIdle = 80 * time.Millisecond
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	s.control.Lock()
	held := s.current != nil
	s.control.Unlock()
	if held {
		t.Fatal("an idle agent still holds the screen")
	}
}

func TestAuditNeverHoldsWhatWasTyped(t *testing.T) {
	s, _, dir := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "type", Text: "my secret reply"})
	act(t, s, "%7", AgentAction{Action: "key", Text: "cmd+v"})
	_, _ = s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{5000, 5000}})
	path := filepath.Join(dir, "screen-agent-audit.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "secret") || !strings.Contains(text, `"text_length":15`) || !strings.Contains(text, `"keys":"cmd+v"`) || !strings.Contains(text, `"error"`) {
		t.Fatalf("audit: %s", text)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode %v", info.Mode())
	}
}

func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The system may count an agent's own events as input: they never pause it.
func TestAnAgentNeverPausesItself(t *testing.T) {
	s, _, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "type", Text: strings.Repeat("long text ", 30)})
	act(t, s, "%7", AgentAction{Action: "left_click_drag", StartCoordinate: []int{10, 10}, Coordinate: []int{300, 200}})
	act(t, s, "%7", AgentAction{Action: "screenshot"})
}

// Someone using the computer itself pauses the agent: what it is doing stops
// with nothing left pressed, and it keeps the screen to carry on after.
func TestSomeoneAtTheComputerPausesTheAgent(t *testing.T) {
	defer func(saved time.Duration) { personPause = saved }(personPause)
	personPause = 500 * time.Millisecond
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})

	done := make(chan error, 1)
	go func() {
		_, err := s.Act(context.Background(), "%7", AgentAction{Action: "hold_key", Text: "shift+a", Duration: 5})
		done <- err
	}()
	waitFor(t, func() bool { _, keys := backend.Snapshot(); return len(keys) == 2 })
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	select {
	case err := <-done:
		if !errors.Is(err, ErrPersonAtComputer) {
			t.Fatalf("hold ended with %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent kept holding keys while someone used the computer")
	}
	if _, keys := backend.Snapshot(); len(keys) != 4 || keys[2] != (FakeKey{'a', false}) || keys[3] != (FakeKey{0xffe1, false}) {
		t.Fatalf("keys %v", keys)
	}
	if _, ok := s.Agent(); !ok {
		t.Fatal("a pause ended the agent's control")
	}

	// A button held between actions comes up when the pause starts.
	time.Sleep(personPause)
	act(t, s, "%7", AgentAction{Action: "left_mouse_down", Coordinate: []int{10, 10}})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("while someone is at the computer: %v", err)
	}
	if pointers, _ := backend.Snapshot(); pointers[len(pointers)-1].Buttons != 0 {
		t.Fatalf("button left down: %v", pointers)
	}
	time.Sleep(personPause)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
}

func TestALockedScreenStopsAgents(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.SetLocked(true)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "screenshot"}); !errors.Is(err, ErrScreenLocked) {
		t.Fatalf("locked: %v", err)
	}
	backend.SetLocked(false)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
}

// An agent reads the focused window as elements it can click, placed on its
// screenshot: what is off screen, empty or secret stays out.
func TestAgentReadsTheFocusedWindow(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.Tree = &ElementTree{App: "Notes", Window: "Inbox", Elements: []Element{
		{Depth: 0, Role: "AXWindow", Title: "Inbox", Frame: image.Rect(0, 0, 2560, 1600), Enabled: true},
		{Depth: 1, Role: "AXGroup", Frame: image.Rect(0, 0, 2560, 1600), Enabled: true},
		{Depth: 2, Role: "AXButton", Title: "New Note", Frame: image.Rect(100, 40, 300, 80), Enabled: true},
		{Depth: 2, Role: "AXTextArea", Value: "明天 10 点\n交周报", Frame: image.Rect(400, 200, 2400, 1500), Enabled: true, Focused: true},
		{Depth: 2, Role: "AXSecureTextField", Title: "Password", Value: "hunter2", Frame: image.Rect(400, 100, 800, 140), Enabled: true},
		{Depth: 2, Role: "AXButton", Title: "Share", Frame: image.Rect(3000, 40, 3100, 80), Enabled: true},
		{Depth: 2, Role: "AXButton", Title: "Delete", Frame: image.Rect(320, 40, 420, 80)},
		{Depth: 2, Role: "AXStaticText", Title: strings.Repeat("长", 100), Frame: image.Rect(10, 1550, 500, 1590), Enabled: true},
	}}
	text := act(t, s, "%7", AgentAction{Action: "elements"}).Text
	for _, want := range []string{
		`Focused app: Notes, window "Inbox"`,
		`  button "New Note" at (100, 30) size 100×20`,
		`  textarea = "明天 10 点 交周报" at (700, 425) size 1000×650, focused`,
		`  securetextfield "Password" at (300, 60) size 200×20`,
		`  button "Delete" at (185, 30) size 50×20, disabled`,
		`statictext "` + strings.Repeat("长", 79) + `…"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"hunter2", "Share", "group"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("%q shown in:\n%s", unwanted, text)
		}
	}

	backend.Tree = nil
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "elements"}); !errors.Is(err, ErrNoElements) {
		t.Fatalf("without accessibility: %v", err)
	}
}

func TestALongWindowIsCut(t *testing.T) {
	tree := ElementTree{App: "Mail"}
	for i := 0; i < 400; i++ {
		tree.Elements = append(tree.Elements, Element{Role: "AXButton", Title: "Message", Frame: image.Rect(0, i*2, 100, i*2+2), Enabled: true})
	}
	text := formatElements(tree, image.Rect(0, 0, 1000, 1000), func(p image.Point) (int, int) { return p.X, p.Y })
	if lines := strings.Count(text, "\n"); lines != 2+elementsShown+1 || !strings.Contains(text, "More elements were left out") {
		t.Fatalf("%d lines:\n%s", lines, text[len(text)-200:])
	}
	if empty := formatElements(ElementTree{App: "Electron"}, image.Rect(0, 0, 10, 10), nil); !strings.Contains(empty, "Use a screenshot") {
		t.Fatalf("empty: %s", empty)
	}
}
