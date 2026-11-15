//go:build !linux

package keeper

import "errors"

// Main is the keeper process; only Linux runs agents' programs this way.
func Main() error { return errors.New("the keeper runs only on Linux") }
