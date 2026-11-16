package screen

import (
	"os"
	"testing"
	"time"
)

// TestMain runs the agent's settle waits at a tenth of a real app's: the fake
// backend redraws at once, so the waits only cost time.
func TestMain(m *testing.M) {
	agentSettle = 40 * time.Millisecond
	elementSettle = 30 * time.Millisecond
	settleFirst = 30 * time.Millisecond
	settleFast = 10 * time.Millisecond
	settleEvery = 6 * time.Millisecond
	settleMax = 250 * time.Millisecond
	os.Exit(m.Run())
}
