//go:build windows

package control

import (
	"net"
	"os"
)

// listenPublished listens on path. Windows keeps binding in place.
func listenPublished(path string) (net.Listener, error) {
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return listener, nil
}
