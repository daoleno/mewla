package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchParentStopsOnceTheParentIsGone(t *testing.T) {
	var current atomic.Int64
	current.Store(4242)
	done := make(chan struct{})
	go watchParent(context.Background(), 4242, func() int { return int(current.Load()) }, time.Millisecond, func() { close(done) })
	select {
	case <-done:
		t.Fatal("stopped while the parent runs")
	case <-time.After(20 * time.Millisecond):
	}
	current.Store(1) // adopted: the app is gone
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not stop once the parent was gone")
	}
}

func TestWatchParentEndsWithTheDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() {
		watchParent(ctx, 4242, func() int { return 4242 }, time.Millisecond, func() { t.Error("stopped while the parent runs") })
		close(ended)
	}()
	cancel()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("still watching after the daemon stopped")
	}
}

func TestStopWithParentNeedsAPid(t *testing.T) {
	for _, value := range []string{"", "x", "1", "0"} {
		t.Setenv(exitWithParentEnv, value)
		stopWithParent(context.Background(), func(error) { t.Errorf("stopped with %q", value) })
	}
	time.Sleep(10 * time.Millisecond)
}
