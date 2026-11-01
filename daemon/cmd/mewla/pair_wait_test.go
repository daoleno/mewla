package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// First-run signs in, starts Mewla and runs pair at once: the relay answered
// "route is not connected" until Mewla registered its route a second later.
func TestPairWaitsForTheRouteToRegister(t *testing.T) {
	clock := time.Unix(0, 0)
	now := func() time.Time { return clock }
	sleep := func(d time.Duration) { clock = clock.Add(d) }
	registeredAt := clock.Add(3 * time.Second)
	calls := 0
	attempt := func() (string, string, error) {
		calls++
		clock = clock.Add(200 * time.Millisecond)
		if clock.Before(registeredAt) {
			return "", "", errors.New("route is not connected")
		}
		return "mewla://settings?v=3&p=AMlink", "Remote access", nil
	}
	var note bytes.Buffer
	link, address, err := requestRemotePairingLink(attempt, remotePairingBudget, now, sleep, &note)
	if err != nil || link != "mewla://settings?v=3&p=AMlink" || address != "Remote access" {
		t.Fatalf("link %q address %q err %v after %d attempts", link, address, err, calls)
	}
	if calls < 3 {
		t.Fatalf("succeeded after %d attempts; the route registered 3 s in", calls)
	}
	if strings.Count(note.String(), "Waiting for remote access to connect") != 1 {
		t.Fatalf("waiting note = %q, want it once", note.String())
	}
}

func TestPairGivesUpWhenTheRouteNeverRegisters(t *testing.T) {
	clock := time.Unix(0, 0)
	now := func() time.Time { return clock }
	sleep := func(d time.Duration) { clock = clock.Add(d) }
	attempt := func() (string, string, error) {
		clock = clock.Add(time.Second)
		return "", "", errors.New("route is not connected")
	}
	_, _, err := requestRemotePairingLink(attempt, remotePairingBudget, now, sleep, &bytes.Buffer{})
	if err == nil || clock.Sub(time.Unix(0, 0)) < remotePairingBudget {
		t.Fatalf("err %v after %s, want it only once the %s budget is spent", err, clock.Sub(time.Unix(0, 0)), remotePairingBudget)
	}
}

func TestPairWithRemoteOffDoesNotWait(t *testing.T) {
	calls := 0
	_, _, err := requestRemotePairingLink(func() (string, string, error) { calls++; return "", "", nil },
		remotePairingBudget, time.Now, func(time.Duration) { t.Fatal("slept with remote access off") }, &bytes.Buffer{})
	if err != nil || calls != 1 {
		t.Fatalf("err %v calls %d", err, calls)
	}
}
