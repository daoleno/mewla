//go:build windows

package platform

import (
	"errors"

	"golang.org/x/sys/windows"
)

// connRefused: Winsock reports WSAECONNREFUSED, not the Errno Go names
// ECONNREFUSED, for a missing socket file or one nobody listens on, and
// WSAENETDOWN when the socket's folder does not exist. An AF_UNIX socket has
// no network that could be down.
func connRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, windows.WSAENETDOWN) ||
		errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}
