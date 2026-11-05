package screen

import (
	"context"
	"errors"
	"image"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two Workers on one computer, each on its own app.
func twoAppService(t *testing.T) (*Service, *FakeBackend) {
	s, backend, _ := newAgentService(t)
	apps := parkingApps()
	apps = append(apps, FakeApp{App: App{Name: "Mail", PID: 404, Windows: []AppWindow{{ID: 9, Bounds: image.Rect(100, 100, 900, 700)}}, Window: image.Rect(100, 100, 900, 700)}, Elements: []Element{
		{Role: "AXWindow", Title: "Inbox", Enabled: true},
		{Depth: 1, Role: "AXTextArea", Title: "Body", Focused: true, Frame: image.Rect(150, 150, 850, 650), Enabled: true},
	}})
	backend.RunningApps = apps
	backend.AgentDisplay = true
	backend.SkyLight, backend.HitTest = true, true
	backend.OnAppClick = func(app *FakeApp, _ FakeAppClick) { app.Elements[len(app.Elements)-1].Value = "x" }
	return s, backend
}

// Concurrency is the point: Workers act at the same time on different apps,
// through clicks sent to the apps and the agent display, and neither waits
// for the other or takes the other's events for a person's.
func TestTwoWorkersActOnDifferentAppsAtTheSameTime(t *testing.T) {
	s, backend := twoAppService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%8", AgentAction{Action: "screenshot"})
	if len(s.Agents()) != 2 {
		t.Fatalf("agents %+v", s.Agents())
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for worker, app := range map[string]string{"%7": "Safari", "%8": "Mail"} {
		worker, app := worker, app
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 12; i++ {
				if _, err := s.Act(context.Background(), worker, AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: app}); err != nil {
					errs <- errors.New(worker + " " + app + ": " + err.Error())
					return
				}
				if _, err := s.Act(context.Background(), worker, AgentAction{Action: "type", Text: "ab", App: app}); err != nil {
					errs <- errors.New(worker + " " + app + " type: " + err.Error())
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the Workers blocked each other")
	}
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	// Each app got only its own Worker's clicks, and both are on the agent display.
	byWindow := map[uint32]int{}
	for _, c := range backend.AppClicks {
		byWindow[c.Window]++
	}
	if byWindow[7] != 12 || byWindow[9] != 12 {
		t.Fatalf("clicks by window %v", byWindow)
	}
	if !backend.Parked[202] || !backend.Parked[404] {
		t.Fatalf("parked %v", backend.Parked)
	}
}

// Neither Worker reads the other's events as a person's, even on the screen
// itself, where the events of one are input that the other did not make.
func TestOneWorkersEventsAreNotAPersonToTheOther(t *testing.T) {
	s, backend := twoAppService(t)
	act(t, s, "%7", AgentAction{Action: "screenshot"})
	act(t, s, "%8", AgentAction{Action: "screenshot"})
	for i := 0; i < 3; i++ {
		act(t, s, "%7", AgentAction{Action: "key", Text: "a"})
		if _, err := s.Act(context.Background(), "%8", AgentAction{Action: "key", Text: "b"}); err != nil {
			t.Fatalf("%%8's key after %%7's: %v", err)
		}
		if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{50, 50}}); err != nil {
			t.Fatalf("%%7's click after %%8's key: %v", err)
		}
	}
	// A real person's input still stops both on the screen itself, and neither
	// on an app that isn't in front.
	time.Sleep(personMargin + 100*time.Millisecond)
	backend.PersonInput()
	for _, worker := range []string{"%7", "%8"} {
		if _, err := s.Act(context.Background(), worker, AgentAction{Action: "key", Text: "c"}); !errors.Is(err, ErrPersonAtComputer) {
			t.Fatalf("%s on the screen while a person types: %v", worker, err)
		}
	}
	if _, err := s.Act(context.Background(), "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"}); err != nil {
		t.Fatalf("an app behind the person's was stopped: %v", err)
	}
	if _, err := s.Act(context.Background(), "%8", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Mail"}); err != nil {
		t.Fatalf("an app behind the person's was stopped: %v", err)
	}
}

// Two Workers don't share an app: the second is told which agent has it.
func TestAWorkerIsToldWhenAnotherHasTheApp(t *testing.T) {
	s, _ := twoAppService(t)
	act(t, s, "%7", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"})
	_, err := s.Act(context.Background(), "%8", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"})
	if err == nil || !strings.Contains(err.Error(), "working in that app") {
		t.Fatalf("second Worker in the same app: %v", err)
	}
	// The first Worker ending frees it.
	s.StopAgent()
	waitFor(t, func() bool { return len(s.Agents()) == 0 })
	act(t, s, "%8", AgentAction{Action: "left_click", Coordinate: []int{100, 80}, App: "Safari"})
}
