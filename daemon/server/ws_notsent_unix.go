//go:build linux || darwin

package server

import (
	"net"

	"golang.org/x/sys/unix"
)

// keepBacklogInOutbox lets the kernel hold at most wsNotSentLowat bytes a
// client has not been sent yet (TCP_NOTSENT_LOWAT). Without it a socket's send
// buffer grows to megabytes, every queued message is in the kernel within
// milliseconds, and the outbox's order (a thread before Work, Calendar and
// pictures) no longer decides what a slow link carries first. Bytes in flight
// are not limited, so a fast link keeps its throughput.
func keepBacklogInOutbox(conn net.Conn) {
	// Under TLS and the listener's peek (tlsHTTPListener) is the TCP socket.
	for depth := 0; depth < 4; depth++ {
		inner, ok := conn.(interface{ NetConn() net.Conn })
		if !ok {
			break
		}
		conn = inner.NetConn()
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_NOTSENT_LOWAT, wsNotSentLowat)
	})
}
