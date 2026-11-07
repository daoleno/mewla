package screen

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Someone using the computer does not stop the agent: the action goes on and
// its answer says what they did and when, so the agent judges for itself. It
// is told once.
func TestThePersonsInputIsToldNotEnforced(t *testing.T) {
	s, backend, dir := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInputOf(InputScroll)
	backend.PersonInputOf(InputClick)
	r, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"})
	if err != nil {
		t.Fatalf("the agent was stopped by the person's input: %v", err)
	}
	for _, want := range []string{"X=", "the person used this computer's own keyboard or pointer", "click", "scroll", "just now", "take a screenshot and judge"} {
		if !strings.Contains(strings.ToLower(r.Text), strings.ToLower(want)) {
			t.Fatalf("answer lacks %q: %s", want, r.Text)
		}
	}
	again := act(t, s, "%7", AgentAction{Action: "cursor_position"})
	if strings.Contains(again.Text, "person used") {
		t.Fatalf("told twice: %s", again.Text)
	}
	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if len(audit) == 0 {
		t.Fatal("no audit")
	}
}

// An app that isn't running is reported as such, whoever is at the computer,
// and nothing is sent anywhere else.
func TestAMissingAppIsReportedAsSuch(t *testing.T) {
	s, backend, _ := newAgentService(t)
	backend.RunningApps = fakeApps()
	act(t, s, "%7", AgentAction{Action: "apps"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	for _, action := range []AgentAction{
		{Action: "app_state", App: "Calendar"},
		{Action: "type", Text: "x", App: "Calendar"},
		{Action: "left_click", Coordinate: []int{10, 10}, App: "Calendar"},
		{Action: "screenshot", App: "Calendar"},
	} {
		_, err := s.Act(context.Background(), "%7", action)
		if err == nil || !strings.Contains(err.Error(), "Calendar") {
			t.Fatalf("%s: %v", action.Action, err)
		}
	}
	if pointers, keys := backend.Snapshot(); len(pointers)+len(keys)+len(backend.AppKeys)+len(backend.AppClicks) != 0 {
		t.Fatal("input went somewhere")
	}
}

// Starting a capture wakes a sleeping display by declaring activity: input
// seen right then is the agent's own, and one after it is the person's.
func TestWakingTheDisplayIsNotAPerson(t *testing.T) {
	defer func(saved time.Duration) { wakeDeclares = saved }(wakeDeclares)
	wakeDeclares = 500 * time.Millisecond
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	backend.SleepDisplay()
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	act(t, s, "%7", AgentAction{Action: "screenshot"}) // a new capture wakes it
	backend.PersonInputOf(InputPointer)
	if r := act(t, s, "%7", AgentAction{Action: "cursor_position"}); strings.Contains(r.Text, "person used") {
		t.Fatalf("the wake-up was told as a person: %s", r.Text)
	}
	time.Sleep(wakeDeclares + personMargin)
	backend.PersonInputOf(InputPointer)
	if r := act(t, s, "%7", AgentAction{Action: "cursor_position"}); !strings.Contains(r.Text, "pointer move") {
		t.Fatalf("after the wake: %s", r.Text)
	}
}

// A display already on isn't woken, so someone typing as an agent's session
// starts is told to it with its very first answer.
func TestAPersonTypingAsASessionStartsIsTold(t *testing.T) {
	defer func(saved time.Duration) { wakeDeclares = saved }(wakeDeclares)
	wakeDeclares = 5 * time.Second
	s, backend, _ := newAgentService(t)
	backend.PersonInput()
	r, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"})
	if err != nil || !strings.Contains(r.Text, "key press") {
		t.Fatalf("first action: %v %q", err, r.Text)
	}
	time.Sleep(personMargin)
	backend.PersonInput()
	if r, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "a"}); err != nil || !strings.Contains(r.Text, "key press") {
		t.Fatalf("while they type: %v %q", err, r.Text)
	}
}

// Keys and clicks an agent sends to one app are its own, even where the
// system counts them as input; a person's after them are told.
func TestAnAgentsAppEventsAreItsOwn(t *testing.T) {
	s, backend, _ := newAgentService(t)
	apps := fakeApps()
	apps[1].Hidden = false
	apps[1].Windows = []AppWindow{{ID: 7, Bounds: image.Rect(0, 0, 400, 300)}}
	backend.RunningApps = apps
	backend.SkyLight = true
	backend.AppEventsSeen = true
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%7", AgentAction{Action: "type", Text: "rent", App: "Safari"})
	if r := act(t, s, "%7", AgentAction{Action: "cursor_position"}); strings.Contains(r.Text, "person used") {
		t.Fatalf("the agent's own keys were told as a person's: %s", r.Text)
	}
	act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 100}, App: "Safari"})
	if r := act(t, s, "%7", AgentAction{Action: "cursor_position"}); strings.Contains(r.Text, "person used") {
		t.Fatalf("the agent's own click was told as a person's: %s", r.Text)
	}
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInputOf(InputClick)
	if r := act(t, s, "%7", AgentAction{Action: "cursor_position"}); !strings.Contains(r.Text, "click") {
		t.Fatalf("a person's click: %s", r.Text)
	}
}

// Nothing waits for the person: an action after their input answers at once.
func TestNothingWaitsForThePerson(t *testing.T) {
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	start := time.Now()
	act(t, s, "%7", AgentAction{Action: "cursor_position"})
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the action waited %v for the person", took)
	}
}

// Input through Watch is the person's too: the viewer's click goes to the
// screen, the agent keeps going and hears about it.
func TestWatchInputIsToldToTheAgent(t *testing.T) {
	s, _, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	s.agentMu.Lock()
	agent := s.agents["%7"]
	s.agentMu.Unlock()
	time.Sleep(personMargin + 50*time.Millisecond)
	agent.watchInput(InputClick)
	r := act(t, s, "%7", AgentAction{Action: "cursor_position"})
	if !strings.Contains(r.Text, "click") || !strings.Contains(r.Text, "person used") {
		t.Fatalf("answer %q", r.Text)
	}
	if again := act(t, s, "%7", AgentAction{Action: "cursor_position"}); strings.Contains(again.Text, "person used") {
		t.Fatalf("told twice: %s", again.Text)
	}
}

// The person taking over is the hard switch: the agent's control ends.
func TestTakingOverEndsTheAgentsControl(t *testing.T) {
	s, _, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	_, cancel := context.WithCancelCause(context.Background())
	person := s.TakeControl(cancel)
	defer s.ReleaseControl(person)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"}); !errors.Is(err, ErrAgentStopped) && !errors.Is(err, ErrPersonControlling) {
		t.Fatalf("after the take over: %v", err)
	}
}

// What the person does in Watch reaches the screen, is told to the agent as
// theirs (a press once, not its release or drag), and never stops it.
func TestWatchPassesThePersonsInputOn(t *testing.T) {
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	s.agentMu.Lock()
	watch := &agentWatch{agent: s.agents["%7"]}
	s.agentMu.Unlock()
	time.Sleep(personMargin + 50*time.Millisecond)
	before, _ := backend.Snapshot()
	for _, p := range []FakePointer{{X: 50, Y: 50}, {X: 50, Y: 50, Buttons: 1}, {X: 60, Y: 60, Buttons: 1}, {X: 60, Y: 60}} {
		if err := watch.Pointer(p.X, p.Y, p.Buttons); err != nil {
			t.Fatal(err)
		}
	}
	if err := watch.Key('q', true); err != nil {
		t.Fatal(err)
	}
	if err := watch.Key('q', false); err != nil {
		t.Fatal(err)
	}
	pointers, keys := backend.Snapshot()
	if len(pointers)-len(before) != 4 || len(keys) != 2 {
		t.Fatalf("not everything reached the screen: %d pointer events, %d keys", len(pointers)-len(before), len(keys))
	}
	r := act(t, s, "%7", AgentAction{Action: "cursor_position"})
	if !strings.Contains(r.Text, "person used") || !strings.Contains(r.Text, "click") || !strings.Contains(r.Text, "key press") {
		t.Fatalf("the agent was not told: %q", r.Text)
	}
	// The agent's own events afterwards are not the person's.
	act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{10, 10}})
	if r := act(t, s, "%7", AgentAction{Action: "cursor_position"}); strings.Contains(r.Text, "person used") {
		t.Fatalf("its own click was told as the person's: %q", r.Text)
	}
}
