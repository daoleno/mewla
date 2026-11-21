package hostdeps

import (
	"slices"
	"testing"
)

func TestAntigravityIsAKnownAgentButNeverPickedForBrain(t *testing.T) {
	if !slices.Contains(AgentOrder(), "agy") || AgentCommand("agy") != "agy" {
		t.Fatalf("agy is a known agent: %v", AgentOrder())
	}
	if slices.Contains(HostAgentOrder(), "agy") {
		t.Fatalf("Mewla must never pick agy for Brain by itself: %v", HostAgentOrder())
	}
	if !slices.Contains(HostAgentOrder(), "claude") {
		t.Fatalf("host order lost its agents: %v", HostAgentOrder())
	}
}
