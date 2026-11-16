package watcher

import (
	"sync"
	"time"
)

// A Session's screen only changes when it prints, and the session host says
// when it last did: its last output time, in whole seconds, comes in the same
// list call that already inventories the Sessions. A Session whose activity has
// not moved since the screen was read, and was read in a later second than that
// activity, shows the same screen now, so it is not read again. An idle Worker
// costs no host call at all; a streaming one is read once per poll as before.
const (
	// paneCaptureRefresh bounds how stale a reused screen can be, in case an
	// activity stamp is wrong or missing.
	paneCaptureRefresh = 30 * time.Second
	// processTableRefresh bounds how stale the process table can be while no
	// pane has changed: a pane's process tree changes through its shell, which
	// prints, so a quiet inventory means a quiet tree. The bound covers what
	// does not print.
	processTableRefresh = 5 * time.Second
	// idlePollAfter and idlePollInterval slow the poll loop while nothing has
	// moved in any pane: a quiet machine is looked at every couple of seconds
	// instead of twice a second, and the first change brings the fast cadence
	// back.
	idlePollAfter    = 3 * time.Second
	idlePollInterval = 2 * time.Second
)

// paneStamp is what the session host's listing says about a Session's screen.
type paneStamp struct {
	activity int64
	dead     bool
	pid      int
}

type cachedScreen struct {
	content    string
	alive      bool
	deadStatus int
	stamp      paneStamp
	// readAtUnix is the second the screen was read in.
	readAtUnix int64
	readAt     time.Time
}

// paneScreens remembers each pane's last screen and the stamps of the latest
// inventory.
type paneScreens struct {
	mu      sync.Mutex
	stamps  map[string]paneStamp
	screens map[string]cachedScreen
	now     func() time.Time

	// inventoryGen moves whenever a listing differs from the one before it.
	inventoryGen uint64
	changedAt    time.Time
	table        map[int]processInfo
	tableGen     uint64
	tableAt      time.Time
}

func newPaneScreens() *paneScreens {
	return &paneScreens{
		changedAt: time.Now(),
		stamps:    map[string]paneStamp{},
		screens:   map[string]cachedScreen{},
		now:       time.Now,
	}
}

// inventory records the stamps of the panes just listed and forgets every pane
// that is gone.
func (p *paneScreens) inventory(panes []sessionPane) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	live := make(map[string]bool, len(panes))
	for _, pane := range panes {
		live[pane.target] = true
		stamp := paneStamp{activity: pane.activity, dead: pane.dead, pid: pane.panePID}
		if old, known := p.stamps[pane.target]; !known || old != stamp {
			p.inventoryGen++
			p.changedAt = p.now()
		}
		p.stamps[pane.target] = stamp
	}
	for target := range p.stamps {
		if !live[target] {
			p.inventoryGen++
			p.changedAt = p.now()
			delete(p.stamps, target)
			delete(p.screens, target)
		}
	}
}

// reuse returns the pane's last screen when nothing could have changed it.
func (p *paneScreens) reuse(target string) (content string, alive bool, deadStatus int, ok bool) {
	if p == nil {
		return "", false, 0, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stamp, listed := p.stamps[target]
	cached, read := p.screens[target]
	if !listed || !read || stamp.activity == 0 || cached.stamp != stamp {
		return "", false, 0, false
	}
	if cached.readAtUnix <= stamp.activity || p.now().Sub(cached.readAt) >= paneCaptureRefresh {
		return "", false, 0, false
	}
	return cached.content, cached.alive, cached.deadStatus, true
}

// readStarted is the clock to compare with a pane's activity: taken before the
// screen is read, so output that lands during the read is never taken as seen.
func (p *paneScreens) readStarted() (time.Time, int64) {
	if p == nil {
		return time.Time{}, 0
	}
	now := p.now()
	return now, now.Unix()
}

func (p *paneScreens) remember(target, content string, alive bool, deadStatus int, startedAt time.Time, startedUnix int64) {
	// A failed read is not a screen.
	if p == nil || (content == "" && !alive && deadStatus == -1) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stamp, listed := p.stamps[target]
	if !listed {
		return
	}
	p.screens[target] = cachedScreen{content: content, alive: alive, deadStatus: deadStatus, stamp: stamp, readAtUnix: startedUnix, readAt: startedAt}
}

// paneRoots are the process ids of the panes in the latest inventory.
func (p *paneScreens) paneRoots() []int {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	roots := make([]int, 0, len(p.stamps))
	for _, stamp := range p.stamps {
		if stamp.pid > 0 {
			roots = append(roots, stamp.pid)
		}
	}
	return roots
}

// processes returns the process table, read fresh only when the pane inventory
// has moved since the last read or the last read is old. The result is shared
// and read-only. A failed read (nil) is never kept.
func (p *paneScreens) processes(read func() map[int]processInfo) map[int]processInfo {
	if p == nil {
		return read()
	}
	p.mu.Lock()
	if p.table != nil && p.tableGen == p.inventoryGen && p.now().Sub(p.tableAt) < processTableRefresh {
		table := p.table
		p.mu.Unlock()
		return table
	}
	gen, at := p.inventoryGen, p.now()
	p.mu.Unlock()

	table := read()
	p.mu.Lock()
	defer p.mu.Unlock()
	if table == nil {
		p.table = nil
		return nil
	}
	p.table, p.tableGen, p.tableAt = table, gen, at
	return table
}

// nextPollDelay is how long to wait before the next poll: the configured
// interval while any pane changed recently, the idle interval once none has.
func (p *paneScreens) nextPollDelay(base time.Duration) time.Duration {
	if p == nil || base >= idlePollInterval {
		return base
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.now().Sub(p.changedAt) < idlePollAfter {
		return base
	}
	return idlePollInterval
}
