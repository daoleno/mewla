package screen

import (
	"context"
	"sync"
	"time"
)

// lockPoll is how often a viewer looks at the computer's lock.
const lockPoll = time.Second

// Observer hears what happens to one viewer's session. Any field may be nil.
type Observer struct {
	// Locked reports the computer's lock when the viewer opens and whenever
	// it changes.
	Locked func(locked bool)
	// Stats is called about once a second while updates flow.
	Stats func(StreamStats)
	// Done is called once, when the session ends.
	Done func(StreamSummary)
}

func (o *Observer) locked(locked bool) {
	if o != nil && o.Locked != nil {
		o.Locked(locked)
	}
}

func (o *Observer) stats(stats StreamStats) {
	if o != nil && o.Stats != nil {
		o.Stats(stats)
	}
}

func (o *Observer) done(summary StreamSummary) {
	if o != nil && o.Done != nil {
		o.Done(summary)
	}
}

// displayHolds counts what needs the display awake: an agent using the screen
// and each phone viewer on an unlocked computer. The backend has one hold, so
// it is on while the count is above zero; without the count an agent ending
// would let go of a viewer's.
type displayHolds struct {
	mu sync.Mutex
	n  int
}

// holdDisplay keeps the display on until the returned release is called, which
// may be more than once.
func (s *Service) holdDisplay() (release func()) {
	holder, ok := s.backend.(displayHolder)
	if !ok {
		return func() {}
	}
	s.holds.mu.Lock()
	s.holds.n++
	if s.holds.n == 1 {
		holder.HoldDisplay(true)
	}
	s.holds.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.holds.mu.Lock()
			s.holds.n--
			if s.holds.n == 0 {
				holder.HoldDisplay(false)
			}
			s.holds.mu.Unlock()
		})
	}
}

// holdWhileViewing keeps the display on for one viewer until ctx ends, except
// while the computer is locked: a lock someone made is theirs to keep, so the
// display may sleep, and it is never held awake to show a login window. The
// first look is synchronous, so a viewer that opens on an unlocked computer
// holds the display before its first frame.
func (s *Service) holdWhileViewing(ctx context.Context, obs *Observer) {
	reporter, _ := s.backend.(presenceReporter)
	var release func()
	var known, wasLocked bool
	look := func() {
		locked := false
		if reporter != nil {
			locked = reporter.Presence().Locked
		}
		if !known || locked != wasLocked {
			known, wasLocked = true, locked
			obs.locked(locked)
		}
		switch {
		case locked && release != nil:
			release()
			release = nil
		case !locked && release == nil:
			release = s.holdDisplay()
		}
	}
	look()
	go func() {
		defer func() {
			if release != nil {
				release()
			}
		}()
		ticker := time.NewTicker(s.lockPollEvery())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				look()
			}
		}
	}()
}

func (s *Service) lockPollEvery() time.Duration {
	if s.lockPoll > 0 {
		return s.lockPoll
	}
	return lockPoll
}
