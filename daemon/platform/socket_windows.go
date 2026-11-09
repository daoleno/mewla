//go:build windows

package platform

import (
	"errors"

	"golang.org/x/sys/windows"
)

// connRefused: Winsock reports WSAECONNREFUSED, not the Errno Go names
// ECONNREFUSED, for a socket file nobody listens on.
func connRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}
