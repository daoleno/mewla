package screen

import (
	"context"
	"fmt"
	"testing"
)

// bigApp is a dense app: one window and n rows of buttons and text.
func denseApp(n int) []FakeApp {
	els := []Element{{Role: "AXWindow", Title: "Big", Enabled: true}}
	for i := 0; i < n; i++ {
		role := "AXButton"
		if i%3 == 0 {
			role = "AXStaticText"
		}
		els = append(els, Element{Depth: 1 + i%5, Role: role, Title: fmt.Sprintf("Item %d", i), Enabled: true, Actions: []string{"AXPress"}})
	}
	return []FakeApp{{App: App{Name: "Big", PID: 900}, Elements: els}}
}

func benchAct(b *testing.B, action AgentAction) {
	backend := NewFakeBackend(2560, 1600)
	s := NewServiceWith(b.TempDir(), backend)
	b.Cleanup(s.StopAgent)
	backend.RunningApps = denseApp(1500)
	if _, err := s.Act(context.Background(), "%9", AgentAction{Action: "apps"}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Act(context.Background(), "%9", action); err != nil {
			b.Fatal(err)
		}
	}
}

// Reading through steps must cost what reading directly does.
func BenchmarkAppState(b *testing.B) { benchAct(b, AgentAction{Action: "app_state", App: "Big"}) }
func BenchmarkStepsExpect(b *testing.B) {
	benchAct(b, AgentAction{Action: "steps", Steps: []AgentAction{{Action: "expect", App: "Big", Match: &ElementMatch{Role: "Button"}}}})
}
func BenchmarkAppStateFind(b *testing.B) {
	benchAct(b, AgentAction{Action: "app_state", App: "Big", Match: &ElementMatch{Role: "Button", Title: "Item 77"}})
}
