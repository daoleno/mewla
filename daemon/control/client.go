package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// ErrNotRunning means no Mewla answers on the control socket: it was never
// started, or it stopped and left the socket behind.
var ErrNotRunning = errors.New("Mewla is not running")

type CallResult struct {
	Response              Response
	RequestMayHaveArrived bool
}

func Call(socketPath string, req Request) (Response, error) {
	return CallWithTimeout(socketPath, req, 30*time.Second)
}

func CallWithTimeout(
	socketPath string,
	req Request,
	timeout time.Duration,
) (Response, error) {
	result, err := CallWithTimeoutResult(socketPath, req, timeout)
	return result.Response, err
}

func CallWithTimeoutResult(
	socketPath string,
	req Request,
	timeout time.Duration,
) (CallResult, error) {
	if socketPath == "" {
		return CallResult{}, fmt.Errorf("control socket path required")
	}
	if timeout <= 0 {
		return CallResult{}, fmt.Errorf("control request timeout required")
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return CallResult{}, fmt.Errorf("%w (%v)", ErrNotRunning, err)
		}
		return CallResult{}, fmt.Errorf("connect to Mewla control socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return CallResult{RequestMayHaveArrived: true}, fmt.Errorf(
			"send control request: %w",
			err,
		)
	}
	result := CallResult{RequestMayHaveArrived: true}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return result, fmt.Errorf("read control response: %w", err)
	}
	result.Response = resp
	return result, nil
}

// PeerPID connects to the control socket and returns the PID the kernel
// records for the process serving it. Unlike anything the daemon says about
// itself, it needs no cooperation from the daemon, so it works against any
// release and cannot be misreported.
func PeerPID(socketPath string) (int, error) {
	if socketPath == "" {
		return 0, fmt.Errorf("control socket path required")
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return 0, fmt.Errorf("%w (%v)", ErrNotRunning, err)
		}
		return 0, fmt.Errorf("connect to Mewla control socket: %w", err)
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("control socket is not a unix connection")
	}
	return peerPID(unixConn)
}
