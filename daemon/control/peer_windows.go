//go:build windows

package control

import (
	"net"

	"github.com/daoleno/mewla/daemon/platform"
)

// peerPID is the system's record of the process on the other end of conn.
func peerPID(conn *net.UnixConn) (int, error) { return platform.UnixPeerPID(conn) }
