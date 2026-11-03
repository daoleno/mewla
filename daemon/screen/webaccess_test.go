package screen

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeWeb struct {
	on, supported bool
	failEnable    bool
	enables       int
	sizes         []int
	polls         int
}

func (f *fakeWeb) ops() webAccessOps {
	return webAccessOps{
		state: func(int) (bool, bool) { return f.on, f.supported },
		enable: func(int) error {
			f.enables++
			if f.failEnable {
				return errors.New("no")
			}
			f.on = true
			return nil
		},
		size: func(context.Context, int) int {
			n := f.sizes[min(f.polls, len(f.sizes)-1)]
			f.polls++
			return n
		},
	}
}

func webClock() (func() time.Time, func(time.Duration)) {
	now := time.Unix(0, 0)
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestWebAccessSwitchesOnceAndSettles(t *testing.T) {
	f := &fakeWeb{supported: true, sizes: []int{5, 5, 40, 90, 90, 90, 90}}
	var w webAccess
	clock, sleep := webClock()
	if note := w.ensure(context.Background(), 7, f.ops(), clock, sleep); note != webAccessNote {
		t.Fatalf("note = %q", note)
	}
	if f.enables != 1 || f.polls > 7 {
		t.Fatalf("enables %d polls %d", f.enables, f.polls)
	}
	if note := w.ensure(context.Background(), 7, f.ops(), clock, sleep); note != "" || f.enables != 1 {
		t.Fatal("a second read must not switch it again")
	}
}

func TestWebAccessLeavesAlone(t *testing.T) {
	clock, sleep := webClock()
	for name, f := range map[string]*fakeWeb{
		"already on":    {on: true, supported: true, sizes: []int{1}},
		"no attribute":  {supported: false, sizes: []int{1}},
		"write refused": {supported: true, failEnable: true, sizes: []int{1}},
	} {
		var w webAccess
		if note := w.ensure(context.Background(), 1, f.ops(), clock, sleep); note != "" {
			t.Errorf("%s: note %q", name, note)
		}
	}
}

func TestWebAccessNotConfirmedIsNotClaimed(t *testing.T) {
	clock, sleep := webClock()
	var w webAccess
	f := &fakeWeb{supported: true, sizes: []int{1}}
	ops := f.ops()
	ops.enable = func(int) error { return nil } // accepted but never read back as on
	if note := w.ensure(context.Background(), 1, ops, clock, sleep); note != "" {
		t.Fatalf("claimed %q", note)
	}
}

func TestWebAccessGivesUpWaiting(t *testing.T) {
	clock, sleep := webClock()
	var w webAccess
	f := &fakeWeb{supported: true, sizes: []int{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24}}
	start := clock()
	if note := w.ensure(context.Background(), 1, f.ops(), clock, sleep); note == "" {
		t.Fatal("expected the note")
	}
	if clock().Sub(start) > webAccessSettle+webAccessTick {
		t.Fatalf("waited %v", clock().Sub(start))
	}
}
