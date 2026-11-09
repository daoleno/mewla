//go:build !linux && !darwin && !windows

package control

import (
	"errors"
	"net"
)

func peerPID(*net.UnixConn) (int, error) {
	return 0, errors.New("peer credentials are not available on this platform")
}
