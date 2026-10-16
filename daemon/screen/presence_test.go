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

// A pause names the input that caused it, in the answer and the audit log,
// so one that won't end shows whether a person, a phantom tap or another
// program is behind it.
func TestAPauseNamesTheInputItSaw(t *testing.T) {
	s, backend, dir := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInputOf(InputScroll)
	backend.PersonInputOf(InputClick)
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"})
	if !errors.Is(err, ErrPersonAtComputer) || !strings.Contains(err.Error(), "(saw a click 0.0 s ago; last ") || !strings.Contains(err.Error(), "scroll 0.0 s ago") {
		t.Fatalf("paused with %v", err)
	}
	audit, _ := os.ReadFile(filepath.Join(dir, "screen-agent-audit.jsonl"))
	if !strings.Contains(string(audit), "saw a click") {
		t.Fatalf("audit %s", audit)
	}
}

// An app that isn't running is reported as such, not as someone at the
// computer, and nothing is sent anywhere else.
func TestAMissingAppIsNotAPause(t *testing.T) {
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
		if err == nil || errors.Is(err, ErrPersonAtComputer) || !strings.Contains(err.Error(), "Calendar") {
			t.Fatalf("%s: %v", action.Action, err)
		}
	}
	if pointers, keys := backend.Snapshot(); len(pointers)+len(keys)+len(backend.AppKeys)+len(backend.AppClicks) != 0 {
		t.Fatal("input went somewhere")
	}
}

// Starting a capture wakes a sleeping display by declaring activity: input
// seen right then is the agent's own.
func TestWakingTheDisplayIsNotAPerson(t *testing.T) {
	defer func(saved time.Duration) { wakeDeclares = saved }(wakeDeclares)
	wakeDeclares = 500 * time.Millisecond
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	backend.SleepDisplay()
	waitFor(t, func() bool { _, ok := s.Agent(); return !ok })
	act(t, s, "%7", AgentAction{Action: "screenshot"}) // a new capture wakes it
	backend.PersonInputOf(InputPointer)
	act(t, s, "%7", AgentAction{Action: "cursor_position"})
	time.Sleep(wakeDeclares + personMargin)
	backend.PersonInputOf(InputPointer)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("after the wake: %v", err)
	}
}

// A display already on isn't woken, so someone typing as an agent's session
// starts pauses its very first action.
func TestAPersonTypingAsASessionStartsComesFirst(t *testing.T) {
	defer func(saved time.Duration) { wakeDeclares = saved }(wakeDeclares)
	wakeDeclares = 5 * time.Second
	s, backend, _ := newAgentService(t)
	backend.PersonInput()
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("first action: %v", err)
	}
	time.Sleep(personMargin)
	backend.PersonInput()
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "key", Text: "a"}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("while they type: %v", err)
	}
}

// An agent waiting for the person keeps its session: a new one would start
// a capture, and wake the display, again.
func TestWaitingForThePersonKeepsTheSession(t *testing.T) {
	s, backend, _ := newAgentService(t)
	s.agentIdle = 400 * time.Millisecond
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	first, _ := s.Agent()
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	for i := 0; i < 8; i++ {
		if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"}); !errors.Is(err, ErrPersonAtComputer) {
			t.Fatalf("try %d: %v", i, err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if now, ok := s.Agent(); !ok || !now.Since.Equal(first.Since) {
		t.Fatalf("the session ended while the agent waited: %v %v", first, now)
	}
}

// Keys and clicks an agent sends to one app are its own, even where the
// system counts them as input; a person's after them still pause it.
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
	act(t, s, "%7", AgentAction{Action: "cursor_position"})
	act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 100}, App: "Safari"})
	act(t, s, "%7", AgentAction{Action: "cursor_position"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInputOf(InputClick)
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"}); !errors.Is(err, ErrPersonAtComputer) {
		t.Fatalf("a person's click: %v", err)
	}
}

// A pause about to end is waited out inside the action, so the agent
// doesn't spend a turn to try again.
func TestAPauseAboutToEndIsWaitedOut(t *testing.T) {
	defer func(saved time.Duration) { personPause = saved }(personPause)
	personPause = 800 * time.Millisecond
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	start := time.Now()
	act(t, s, "%7", AgentAction{Action: "cursor_position"})
	if waited := time.Since(start); waited < 500*time.Millisecond {
		t.Fatalf("acted %v after the person, inside the pause", waited)
	}
}

// A pause with longer left is answered at once, with the time left and the
// work that goes on meanwhile.
func TestALongPauseIsAnsweredAtOnce(t *testing.T) {
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	start := time.Now()
	_, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"})
	if !errors.Is(err, ErrPersonAtComputer) || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("after %v: %v", time.Since(start), err)
	}
	if text := err.Error(); !strings.Contains(text, "Try again in about 10 s") || !strings.Contains(text, "app_state, element and steps on it go on") {
		t.Fatalf("answer %q", text)
	}
}

// The person taking over ends a wait at once.
func TestTakingOverEndsTheWait(t *testing.T) {
	defer func(saved time.Duration) { personPause = saved }(personPause)
	personPause = 2500 * time.Millisecond
	s, backend, _ := newAgentService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	time.Sleep(personMargin + 50*time.Millisecond)
	backend.PersonInput()
	done := make(chan error, 1)
	go func() {
		_, err := s.Act(context.Background(), "%7", AgentAction{Action: "cursor_position"})
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	_, cancel := context.WithCancelCause(context.Background())
	person := s.TakeControl(cancel)
	defer s.ReleaseControl(person)
	if err := <-done; !errors.Is(err, ErrAgentStopped) || time.Since(start) > time.Second {
		t.Fatalf("after %v: %v", time.Since(start), err)
	}
}
