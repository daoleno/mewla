//go:build !windows

package server

import "syscall"

var platformPeerGoneErrnos []syscall.Errno
