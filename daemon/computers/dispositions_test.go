package computers_test

import (
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/computers"
)

// The daemon passes brain.SubmitExternalUserInput's disposition through as a
// string; the channel's retry rules depend on these staying the same words.
func TestDispositionsMatchBrain(t *testing.T) {
	pairs := map[brain.ExternalInputDisposition]string{
		brain.ExternalInputAccepted:     computers.DispositionAccepted,
		brain.ExternalInputNotSubmitted: computers.DispositionNotSubmitted,
		brain.ExternalInputUncertain:    computers.DispositionUncertain,
		brain.ExternalInputPending:      computers.DispositionPending,
	}
	for fromBrain, onChannel := range pairs {
		if string(fromBrain) != onChannel {
			t.Fatalf("brain says %q, channel expects %q", fromBrain, onChannel)
		}
	}
}
