package server

import (
	"errors"
	"syscall"
)

// isPeerGone reports whether a socket write failed because the other end is
// no longer there (it hung up or reset the connection), as opposed to the
// daemon giving up on a client: a write timeout, a full outbox or its own
// close are not this. The same case has a different errno on each OS; the
// Windows ones are in ws_peer_gone_windows.go.
func isPeerGone(err error) bool {
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	for _, errno := range platformPeerGoneErrnos {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
