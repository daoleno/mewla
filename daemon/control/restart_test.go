package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type answerHandler struct{}

func (answerHandler) HandleControlRequest(Request) Response {
	return Response{OK: true, Text: "back"}
}

// A command issued while the daemon restarts waits for the new one (11-19:
// "Mewla isn't running here" during every dev reload).
func TestCallWaitsOutARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), SocketName)
	if err := MarkRestarting(path, os.Getpid(), time.Now().Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &Server{Path: path, Handler: answerHandler{}}
	go func() {
		time.Sleep(700 * time.Millisecond)
		_ = server.Run(ctx)
	}()
	started := time.Now()
	resp, err := Call(path, Request{Type: "computers"})
	if err != nil || resp.Text != "back" {
		t.Fatalf("call during restart: %+v, %v", resp, err)
	}
	if waited := time.Since(started); waited < 500*time.Millisecond {
		t.Fatalf("answered after %v, before the new daemon listened", waited)
	}
	if _, err := os.Stat(path + restartMarkerSuffix); !os.IsNotExist(err) {
		t.Fatalf("the new daemon left the restart marker: %v", err)
	}
}

// A marker whose process is gone, or whose time is up, does not delay "not
// running".
func TestStaleRestartMarkerDoesNotDelay(t *testing.T) {
	for name, marker := range map[string]struct {
		pid   int
		until time.Duration
	}{
		"process gone": {pid: 1 << 30, until: time.Minute},
		"time up":      {pid: os.Getpid(), until: -time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), SocketName)
			if err := MarkRestarting(path, marker.pid, time.Now().Add(marker.until)); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if _, err := Call(path, Request{Type: "computers"}); !errors.Is(err, ErrNotRunning) {
				t.Fatalf("want not running, got %v", err)
			}
			if waited := time.Since(started); waited > 300*time.Millisecond {
				t.Fatalf("a stale marker delayed the answer by %v", waited)
			}
		})
	}
}
