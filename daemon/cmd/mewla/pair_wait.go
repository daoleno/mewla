package main

import (
	"fmt"
	"io"
	"time"
)

// remotePairingBudget is how long mewla pair keeps asking for a remote access
// admission. Right after sign-in the computer's Mewla is (re)started and
// registers its route with the relay a second or two later; a pair that runs in
// between is refused with "route is not connected" until then. The first-run
// flow runs pair exactly there, so one retry was not enough.
const remotePairingBudget = 45 * time.Second

// requestRemotePairingLink calls attempt until it succeeds or the budget is
// spent, backing off between tries, and says once that it is waiting. An
// attempt that returns no link and no error (remote access is off) ends it.
func requestRemotePairingLink(
	attempt func() (link, address string, err error),
	budget time.Duration,
	now func() time.Time,
	sleep func(time.Duration),
	note io.Writer,
) (string, string, error) {
	start := now()
	delay := 500 * time.Millisecond
	waited := false
	for {
		link, address, err := attempt()
		if err == nil {
			return link, address, nil
		}
		if now().Sub(start) >= budget {
			return "", "", err
		}
		if !waited {
			waited = true
			fmt.Fprintln(note, "Waiting for remote access to connect…")
		}
		sleep(delay)
		if delay < 4*time.Second {
			delay *= 2
		}
	}
}
