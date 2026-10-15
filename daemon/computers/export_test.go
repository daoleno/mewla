package computers

import "time"

// FastTimings shortens retry and redial delays for tests.
func FastTimings() func() {
	saved := []time.Duration{retryPending, retryNotSubmitted, sendTick, redialMin}
	retryPending, retryNotSubmitted, sendTick, redialMin = 50*time.Millisecond, 50*time.Millisecond, 50*time.Millisecond, 50*time.Millisecond
	return func() {
		retryPending, retryNotSubmitted, sendTick, redialMin = saved[0], saved[1], saved[2], saved[3]
	}
}
