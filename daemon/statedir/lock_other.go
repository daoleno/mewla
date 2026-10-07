//go:build !linux && !darwin

package statedir

import (
	"errors"
	"os"
)

var errExchangeUnsupported = errors.New("atomic exchange unsupported")

func exchange(a, b string) error { return errExchangeUnsupported }

// Without flock there is no reliable running-daemon probe; never move.
func lockDaemons(root string) ([]*os.File, string, error) {
	return nil, root, nil
}

func releaseLocks([]*os.File) {}

func isUnsupportedMove(error) bool { return false }
