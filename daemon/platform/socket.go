package platform

import (
	"errors"
	"os"
	"syscall"
)

// NoListener reports whether dialing a local (AF_UNIX) socket failed because
// nothing serves it: no socket file, or a file nobody listens on.
func NoListener(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) || connRefused(err)
}
