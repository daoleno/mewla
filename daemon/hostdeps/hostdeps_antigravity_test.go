package hostdeps

import (
	"slices"
	"testing"
)

func TestAntigravityIsAKnownAgentAndNeverTheFirstChoice(t *testing.T) {
	order := AgentOrder()
	if !slices.Contains(order, "agy") || AgentCommand("agy") != "agy" {
		t.Fatalf("agy is a known agent: %v", order)
	}
	// agy hosts Brain only when it is the one agent that is ready or the person
	// picks it: it comes after every agent with hooks and approval handling.
	if order[len(order)-1] != "agy" {
		t.Fatalf("agy must be the last choice: %v", order)
	}
}
