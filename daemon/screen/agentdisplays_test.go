package screen

import (
	"context"
	"errors"
	"testing"
)

// perAgentBackend is a backend whose agents each have a display of their own,
// as Linux's do.
type perAgentBackend struct{ unsupportedBackend }

func (perAgentBackend) Capability(context.Context) Capability { return Capability{Supported: true} }
func (perAgentBackend) StartFor(context.Context, string) (Session, error) {
	return nil, errors.New("not in this test")
}

func TestAgentDisplaysAreNotTheScreen(t *testing.T) {
	s := NewServiceWith(t.TempDir(), perAgentBackend{})
	status := s.Status(context.Background(), "device")
	if status.Supported || !status.AgentDisplays || status.Reason == "" {
		t.Fatalf("a computer whose agents have displays of their own has no screen to view: %+v", status)
	}
	if _, err := s.AgentReady(context.Background()); err != nil {
		t.Fatalf("agents can still use it: %v", err)
	}
}
