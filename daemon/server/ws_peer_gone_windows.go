//go:build windows

package server

import "syscall"

// Winsock reports a hung-up peer as its own codes, not the POSIX ones.
var platformPeerGoneErrnos = []syscall.Errno{
	syscall.WSAECONNRESET,   // 10054
	syscall.WSAECONNABORTED, // 10053
	syscall.ERROR_BROKEN_PIPE,
	syscall.ERROR_NETNAME_DELETED,
}
