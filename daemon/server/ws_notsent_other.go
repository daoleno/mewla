//go:build !linux && !darwin

package server

import "net"

// keepBacklogInOutbox is TCP_NOTSENT_LOWAT where the platform has it (see
// ws_notsent_unix.go); elsewhere the kernel's send buffer decides.
func keepBacklogInOutbox(net.Conn) {}
