package screen

import (
	"context"
	"time"
)

// AppPrint is a cheap fingerprint of an app's windows, to tell whether it is
// still changing: how many windows it has, how many elements its front
// window holds directly, which element has the focus and the window's title.
// It misses a change deeper down, so what must be found is read, not
// assumed.
type AppPrint struct {
	Windows  int
	Children int
	Focus    uintptr
	Title    string
}

// appPrinter is a backend that can fingerprint an app in a request or two,
// far cheaper than reading it.
type appPrinter interface {
	PrintApp(ctx context.Context, pid int) (AppPrint, error)
}

// The app is given settleFirst to start reacting, then fingerprinted every
// settleEvery until two in a row agree, for at most settleMax. waitForRecheck
// is how long wait_for trusts an unchanged fingerprint before it reads the
// app anyway.
var (
	settleFirst    = 300 * time.Millisecond
	settleEvery    = 100 * time.Millisecond
	settleMax      = 2500 * time.Millisecond
	waitForRecheck = time.Second
)

// settler waits for something to go quiet.
type settler struct {
	first, every, max time.Duration
	now               func() time.Time
	sleep             func(ctx context.Context, d time.Duration) error
	print             func(ctx context.Context) (AppPrint, error)
}

// quiet waits first, then fingerprints until two reads in a row agree. It
// gives up at max in all, settled false, and when a fingerprint can't be
// read (the app is gone or not answering), which the caller's own read
// then reports. An error is the wait itself being ended.
func (s settler) quiet(ctx context.Context) (settled bool, err error) {
	start := s.now()
	left := func() time.Duration { return s.max - s.now().Sub(start) }
	if s.first > 0 {
		if err := s.sleep(ctx, min(s.first, max(left(), 0))); err != nil {
			return false, err
		}
	}
	previous, perr := s.print(ctx)
	if perr != nil {
		return false, nil
	}
	for left() > 0 {
		if err := s.sleep(ctx, min(s.every, left())); err != nil {
			return false, err
		}
		current, perr := s.print(ctx)
		if perr != nil {
			return false, nil
		}
		if current == previous {
			return true, nil
		}
		previous = current
	}
	return false, nil
}

// sleep waits d, ended by the call's context or the agent's control ending.
func (a *agentSession) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-a.ctx.Done():
		return context.Cause(a.ctx)
	}
}

// settlerFor fingerprints the app with pid, or is nil when the backend can't.
func (a *agentSession) settlerFor(pid int, first, limit time.Duration) *settler {
	printer, ok := a.apps.(appPrinter)
	if !ok {
		return nil
	}
	return &settler{first: first, every: settleEvery, max: limit, now: time.Now, sleep: a.sleep,
		print: func(ctx context.Context) (AppPrint, error) { return printer.PrintApp(ctx, pid) }}
}

// settleApp lets an app redraw after an action on it: until it is quiet, or
// for a fixed moment where it can't be fingerprinted.
func (a *agentSession) settleApp(ctx context.Context, pid int) error {
	if st := a.settlerFor(pid, settleFirst, settleMax); st != nil {
		_, err := st.quiet(ctx)
		return err
	}
	return a.sleep(ctx, elementSettle)
}

// awaitChange waits for the app to look different from before, for wait_for:
// a changed fingerprint, once it has gone quiet, or waitForRecheck, or
// deadline. Without a fingerprint it waits waitForEvery. It returns when a
// full read is worth taking.
func (a *agentSession) awaitChange(ctx context.Context, pid int, before AppPrint, known bool, deadline time.Time) error {
	st := a.settlerFor(pid, 0, 0)
	if st == nil || !known {
		return a.sleep(ctx, waitForEvery)
	}
	recheck := time.Now().Add(waitForRecheck)
	for {
		stop := recheck
		if deadline.Before(stop) {
			stop = deadline
		}
		wait := min(settleEvery, time.Until(stop))
		if wait <= 0 {
			return nil
		}
		if err := a.sleep(ctx, wait); err != nil {
			return err
		}
		current, err := st.print(ctx)
		if err != nil {
			return nil
		}
		if current != before {
			st.max = max(min(settleMax, time.Until(deadline)), 0)
			_, err := st.quiet(ctx)
			return err
		}
	}
}
