package hostdeps

import (
	"slices"
	"testing"
)

func TestAntigravityRunsWorkersButNeverHostsBrain(t *testing.T) {
	if !slices.Contains(AgentOrder(), "agy") || AgentCommand("agy") != "agy" {
		t.Fatalf("agy is a known agent: %v", AgentOrder())
	}
	if slices.Contains(HostAgentOrder(), "agy") || CanHostBrain("agy") || !CanHostBrain("claude") {
		t.Fatalf("host order %v", HostAgentOrder())
	}
}
