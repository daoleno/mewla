package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sleeper(t *testing.T, seconds string) *child {
	t.Helper()
	p, err := spawn(os.Environ(), "sleep", seconds)
	if err != nil {
		t.Skipf("sleep is unavailable: %v", err)
	}
	t.Cleanup(p.stop)
	return p
}

func TestWaitForFileSeesAFileCreatedLater(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ready")
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = os.WriteFile(target, []byte("x"), 0o600)
	}()
	started := time.Now()
	err := waitForFile(context.Background(), 5*time.Second, dir, sleeper(t, "30"), func() bool { _, e := os.Stat(target); return e == nil })
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("took %v", time.Since(started))
	}
}

func TestWaitForFileRechecksAfterAPartialWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "DevToolsActivePort")
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = os.WriteFile(target, nil, 0o600)
		time.Sleep(60 * time.Millisecond)
		_ = os.WriteFile(target, []byte("9222\n/devtools/browser/x\n"), 0o600)
	}()
	var port string
	err := waitForFile(context.Background(), 5*time.Second, dir, sleeper(t, "30"), func() bool {
		data, e := os.ReadFile(target)
		if e != nil || !strings.Contains(string(data), "\n") {
			return false
		}
		port = strings.Split(string(data), "\n")[0]
		return true
	})
	if err != nil || port != "9222" {
		t.Fatalf("port=%q err=%v", port, err)
	}
}

func TestWaitForFileReturnsAtOnceWhenTheCreatorExits(t *testing.T) {
	dir := t.TempDir()
	creator, err := spawn(os.Environ(), "true")
	if err != nil {
		t.Skip(err)
	}
	started := time.Now()
	err = waitForFile(context.Background(), 10*time.Second, dir, creator, func() bool { return false })
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("a dead launcher was waited on for %v", time.Since(started))
	}
}

func TestWaitForFileHonoursItsTimeoutAndContext(t *testing.T) {
	dir := t.TempDir()
	creator := sleeper(t, "30")
	if err := waitForFile(context.Background(), 80*time.Millisecond, dir, creator, func() bool { return false }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForFile(ctx, 10*time.Second, dir, creator, func() bool { return false }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if err := waitForFile(context.Background(), time.Second, dir, creator, func() bool { return true }); err != nil {
		t.Fatalf("already there: %v", err)
	}
}

func TestWaitForExitWakesWhenTheProcessEnds(t *testing.T) {
	cmd := exec.Command("sleep", "0.1")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	p := &child{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	started := time.Now()
	if err := waitForExit(context.Background(), 5*time.Second, p); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("took %v", time.Since(started))
	}
	long := sleeper(t, "30")
	if err := waitForExit(context.Background(), 50*time.Millisecond, long); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}
