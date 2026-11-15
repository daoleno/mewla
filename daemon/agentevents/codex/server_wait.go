package codex

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// serverWaitTimeout bounds how long a launch waits for its app-server. A
// warm app-server listens within about 50 ms; a first start after login, or
// one a virus scanner holds, takes longer.
const serverWaitTimeout = 15 * time.Second

// WaitForServer returns once the app-server at socket accepts a connection,
// or with an error after timeout. The connection is closed at once; the
// app-server serves each client on its own.
func WaitForServer(ctx context.Context, socket string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("Codex's app-server at %s did not accept a connection within %s: %w", socket, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// WaitMain is `mewla codex-server-wait SOCKET`. A Codex launch runs it
// between starting its app-server in the background and starting the TUI
// against it: `codex --remote` makes one attempt to connect and exits 1 when
// the app-server is not listening yet, which on Windows it often was not.
func WaitMain(args []string, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: mewla codex-server-wait SOCKET")
		return 2
	}
	if err := WaitForServer(context.Background(), args[0], serverWaitTimeout); err != nil {
		fmt.Fprintf(stderr, "mewla: %v; its log is %s\n", err, ControlLogPath(args[0]))
		return 1
	}
	return 0
}
