package watcher

import (
	"errors"
	"strings"
	"testing"
)

func TestKillSessionMissingIsIdempotentSuccess(t *testing.T) {
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	if err := w.KillSession("%1"); err != nil {
		t.Fatalf("missing target must be idempotent success: %v", err)
	}
	if kills := backend.kills(); len(kills) != 0 {
		t.Fatalf("killed a missing Session: %v", kills)
	}
}

func TestKillSessionResourceReleaseFailureAfterSuccessfulKill(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{
		boundTarget: "%42",
		boundUnit:   unit,
		releaseErr:  errors.New("injected cgroup release failure"),
	}
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	backend.set("%42", fakeOwned)
	w.resources = manager
	err := w.KillSession("%42")
	if err == nil || !errors.Is(err, ErrDelegatedResourceRelease) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "injected cgroup release failure") {
		t.Fatalf("err=%v", err)
	}
	if len(manager.released) != 1 || len(backend.kills()) != 1 {
		t.Fatalf("released=%#v killed=%v", manager.released, backend.kills())
	}
}

func TestKillSessionMissingStillReleasesBoundUnit(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{boundTarget: "%42", boundUnit: unit}
	w := New(0)
	useFakeSessionBackend(w, newFakeSessionBackend())
	w.resources = manager
	if err := w.KillSession("%42"); err != nil {
		t.Fatal(err)
	}
	if len(manager.released) != 1 {
		t.Fatalf("released=%#v", manager.released)
	}
}

func TestKillSessionRetryAfterResourceFailureConverges(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{
		boundTarget: "%42",
		boundUnit:   unit,
		releaseErr:  errors.New("first release fail"),
	}
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	backend.set("%42", fakeOwned)
	w.resources = manager
	if err := w.KillSession("%42"); !errors.Is(err, ErrDelegatedResourceRelease) {
		t.Fatalf("first=%v", err)
	}
	manager.releaseErr = nil
	if err := w.KillSession("%42"); err != nil {
		t.Fatalf("retry=%v", err)
	}
	if len(manager.released) != 2 {
		t.Fatalf("released=%#v", manager.released)
	}
}

func TestKillSessionNeverEndsAnUnownedSession(t *testing.T) {
	w := New(0)
	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	backend.set("%1", fakeUnowned)
	if err := w.KillSession("%1"); !errors.Is(err, ErrUnownedSession) {
		t.Fatalf("err=%v, want ErrUnownedSession", err)
	}
	if kills := backend.kills(); len(kills) != 0 {
		t.Fatalf("killed an unowned Session: %v", kills)
	}
}

func TestProbeSessionDistinguishesAbsentFromTransportError(t *testing.T) {
	w := New(0)
	// No session host running: nothing is.
	presence, err := w.ProbeSession("mh-0123456789abcdef01234567")
	if err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("no-host presence=%v err=%v", presence, err)
	}

	backend := useFakeSessionBackend(w, newFakeSessionBackend())
	presence, err = w.ProbeSession("%1")
	if err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("missing presence=%v err=%v", presence, err)
	}

	backend.set("%1", fakeUnreachable)
	presence, err = w.ProbeSession("%1")
	if presence != SessionPresenceUnknown || !errors.Is(err, ErrOwnershipProbeUnavailable) {
		t.Fatalf("transport presence=%v err=%v", presence, err)
	}

	backend.set("%1", fakeOwned)
	presence, err = w.ProbeSession("%1")
	if err != nil || presence != SessionPresencePresent {
		t.Fatalf("owned presence=%v err=%v", presence, err)
	}
}
