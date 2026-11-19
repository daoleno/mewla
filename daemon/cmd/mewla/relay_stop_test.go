//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles"
)

// 11-19 03:25: a Ctrl-C on mewla-dev reached the daemon twice (the terminal's
// process group, then the runner's forward) and the second copy cut every
// reply at once. The same stop arriving again at once keeps the drain; a later
// signal still cuts it.
func TestTheSameStopDeliveredTwiceDoesNotCutTheDrain(t *testing.T) {
	// Keep SIGINT from ending the test binary while no handler is registered.
	guard := make(chan os.Signal, 4)
	signal.Notify(guard, syscall.SIGINT)
	defer signal.Stop(guard)

	draining := make(chan struct{})
	cutAfter := make(chan time.Duration, 1)
	var r relayStop
	r.set(func(ctx context.Context) modelprofiles.DrainResult {
		started := time.Now()
		close(draining)
		<-ctx.Done()
		cutAfter <- time.Since(started)
		return modelprofiles.DrainResult{Cut: 1}
	}, "")
	requested, stop := context.WithCancelCause(context.Background())
	ended := make(chan error, 1)
	go r.run(requested, func(cause error) { ended <- cause })
	stop(errors.New("interrupt"))
	<-draining

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case after := <-cutAfter:
		t.Fatalf("the forwarded copy of the stop cut the drain after %s", after)
	case <-time.After(sameStopWindow + 200*time.Millisecond):
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cutAfter:
	case <-time.After(5 * time.Second):
		t.Fatal("a later signal did not cut the drain")
	}
	if cause := <-ended; cause == nil || cause.Error() != "interrupt" {
		t.Fatalf("runtime ended with %v", cause)
	}
}
