//go:build !linux

package gc

import "errors"

// WhyProcess reads /proc, which this platform does not have: only paths are
// told here.
func WhyProcess(WhyLookup, int) (Why, error) {
	return Why{}, errors.New("mewla gc why PID is not available on this platform yet; give a path")
}
