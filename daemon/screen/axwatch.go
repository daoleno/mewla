package screen

import (
	"context"
	"os"
	"time"
)

// An app tells when it changes, if asked: macOS accessibility notifications
// (value changed, focus moved, window created, title changed). Settling on
// them returns when the app has gone quiet after the action, not after a
// guessed wait. It is off unless MEWLA_SCREEN_AX_OBSERVER is set, until
// measured on a real Mac against the polling settle (see settle.go).

// appWatcher is a backend that can listen to an app's change notifications.
type appWatcher interface {
	// WatchApp starts listening to pid. Events arrive until Close; a slow
	// reader loses events, never blocks the app.
	WatchApp(pid int) (AppWatch, error)
}

// AppWatch is one listener on an app.
type AppWatch interface {
	Events() <-chan struct{}
	Close()
}

var axObserverOn = os.Getenv("MEWLA_SCREEN_AX_OBSERVER") != ""

// Quiet by notification: wait up to watchFirst for the app's first sign of
// change, then until watchQuiet passes with none, never longer than watchMax
// in all.
var (
	watchFirst = 40 * time.Millisecond
	watchQuiet = 60 * time.Millisecond
	watchMax   = 1500 * time.Millisecond
)

// waitQuiet drains events and says whether any came. With none within first it
// returns false (the action showed nothing the app announced); otherwise it
// returns true once quiet has passed since the last, or at max.
func waitQuiet(ctx context.Context, events <-chan struct{}, first, quiet, max time.Duration) (bool, error) {
	deadline := time.NewTimer(max)
	defer deadline.Stop()
	wait := time.NewTimer(first)
	defer wait.Stop()
	saw := false
	for {
		select {
		case <-events:
			if !saw {
				saw = true
			}
			if !wait.Stop() {
				select {
				case <-wait.C:
				default:
				}
			}
			wait.Reset(quiet)
		case <-wait.C:
			return saw, nil
		case <-deadline.C:
			return saw, nil
		case <-ctx.Done():
			return saw, ctx.Err()
		}
	}
}
