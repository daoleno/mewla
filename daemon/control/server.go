package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Server struct {
	Path    string
	Handler Handler
}

func (s *Server) Run(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("control server required")
	}
	if s.Handler == nil {
		return fmt.Errorf("control handler required")
	}
	if s.Path == "" {
		return fmt.Errorf("control socket path required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create control socket dir: %w", err)
	}
	if err := removeStaleSocket(s.Path); err != nil {
		return err
	}
	listener, err := listenPublished(s.Path)
	if err != nil {
		return fmt.Errorf("listen control socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(s.Path)
	// Listening again: commands waiting out a restart reach this daemon.
	ClearRestarting(s.Path)

	var connectionMu sync.Mutex
	connections := make(map[net.Conn]struct{})
	stopped := false
	var handlers sync.WaitGroup
	runDone := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			connectionMu.Lock()
			stopped = true
			_ = listener.Close()
			for conn := range connections {
				_ = conn.Close()
			}
			connectionMu.Unlock()
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-runDone:
		}
	}()
	defer func() {
		close(runDone)
		stop()
		handlers.Wait()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept control connection: %w", err)
		}
		connectionMu.Lock()
		if stopped {
			connectionMu.Unlock()
			_ = conn.Close()
			continue
		}
		connections[conn] = struct{}{}
		handlers.Add(1)
		connectionMu.Unlock()
		go func() {
			defer handlers.Done()
			defer func() {
				connectionMu.Lock()
				delete(connections, conn)
				connectionMu.Unlock()
			}()
			s.handleConn(conn)
		}()
	}
}

// slowAuthRequests are the quick auth requests the CLI waits on with a short
// timeout (mewla pair, web, devices); one slower than slowAuthRequest is
// logged so a CLI timeout can be explained from the daemon log.
var slowAuthRequests = map[string]bool{"pair": true, "device_list": true, "device_revoke": true}

const slowAuthRequest = 500 * time.Millisecond

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	var req Request
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(ErrorResponse("invalid_request", err.Error()))
		return
	}
	started := time.Now()
	resp := s.Handler.HandleControlRequest(req)
	if slowAuthRequests[req.Type] {
		if took := time.Since(started); took > slowAuthRequest {
			log.Printf("control: %s took %s", req.Type, took.Round(time.Millisecond))
		}
	}
	if !resp.OK && resp.Error == nil {
		resp.Error = &Error{Code: "request_failed", Message: "Request failed."}
	}
	_ = json.NewEncoder(conn).Encode(resp)
	if resp.After != nil {
		_ = conn.Close()
		resp.After()
	}
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat control socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("control socket path exists and is not a socket: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale control socket: %w", err)
	}
	return nil
}
