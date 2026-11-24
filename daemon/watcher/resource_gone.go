//go:build linux || darwin

package watcher

import (
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// A Session can end without KillSession: its session host stops or crashes
// (a reboot, an OOM kill, a host that was replaced), and every Session in it
// goes with it. Its lease and scratch then stayed forever, because only
// KillSession released them. releaseGone releases them, but only on positive
// evidence that the Session is gone, never on a short or unreadable inventory.
//
// The evidence for a unit, all of it required:
//   - the unit is on no Session (alive or exited) in two successful
//     inventories at least goneConfirm apart, both from the same connected
//     session host (same pid and start time); a host that changes restarts the
//     count;
//   - the lease is older than goneConfirm and is not a reservation of this
//     process (a Prepare not yet Bound);
//   - none of the processes the lease owns is a direct child of that host,
//     which is what a live pane is (read fresh, just before the release).
const (
	goneScanEvery      = 30 * time.Second
	goneConfirm        = 2 * time.Minute
	trashFinalizeEvery = time.Hour
)

type goneEvidence struct {
	host        sessionHostIdentity
	firstAbsent time.Time
	seen        int
}

type goneLeases struct {
	lastScan     time.Time
	lastFinalize time.Time
	absent       map[string]goneEvidence
	releasing    bool
	// passes counts the goroutines releaseGone started that have not ended.
	// releasing is set only while a release is due, so a pass that only runs
	// the collector is not told by it; tests join on this.
	passes sync.WaitGroup
	// unread is the last list of t/ items the sweep kept because their file
	// system records no reads; it is logged when it changes.
	unread string
}

// goneCandidates records one inventory's evidence and returns the units whose
// Sessions are proven gone. The caller holds no lock.
func (m *portableDelegatedResourceManager) goneCandidates(windows []sessionPane, host sessionHostIdentity, now time.Time) []workerproc.Lease {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !host.known() || m.gone.releasing {
		return nil
	}
	if !m.gone.lastScan.IsZero() && now.Sub(m.gone.lastScan) < goneScanEvery {
		return nil
	}
	m.gone.lastScan = now
	if m.gone.absent == nil {
		m.gone.absent = map[string]goneEvidence{}
	}
	carried := make(map[string]bool, len(windows))
	for _, window := range windows {
		if window.resourceUnit != "" {
			carried[window.resourceUnit] = true
		}
	}
	leases, err := workerproc.ListLeases(m.leaseDir)
	if err != nil {
		log.Printf("watcher: read leases: %v", err)
		return nil
	}
	listed := make(map[string]bool, len(leases))
	var due []workerproc.Lease
	for _, lease := range leases {
		unit := lease.ResourceID
		listed[unit] = true
		if !validDelegatedResourceUnit(m.owner, unit) || carried[unit] {
			delete(m.gone.absent, unit)
			continue
		}
		if until, ok := m.reserved[unit]; ok && now.Before(until) {
			continue
		}
		if !lease.StartedAt.IsZero() && now.Sub(lease.StartedAt) < goneConfirm {
			continue
		}
		evidence, ok := m.gone.absent[unit]
		if !ok || evidence.host != host {
			m.gone.absent[unit] = goneEvidence{host: host, firstAbsent: now, seen: 1}
			continue
		}
		evidence.seen++
		m.gone.absent[unit] = evidence
		if evidence.seen >= 2 && now.Sub(evidence.firstAbsent) >= goneConfirm {
			due = append(due, lease)
		}
	}
	for unit := range m.gone.absent {
		if !listed[unit] {
			delete(m.gone.absent, unit)
		}
	}
	sort.Slice(due, func(a, b int) bool { return due[a].ResourceID < due[b].ResourceID })
	if len(due) > 0 {
		m.gone.releasing = true
	}
	return due
}

// releaseGone releases the leases whose Sessions are proven gone, and deletes
// trash past its grace. It runs in its own goroutine so a poll never waits on
// a process stop; one runs at a time.
func (m *portableDelegatedResourceManager) releaseGone(windows []sessionPane, host sessionHostIdentity) {
	now := m.clock()
	due := m.goneCandidates(windows, host, now)
	finalize := m.finalizeDue(now)
	if len(due) == 0 && !finalize {
		return
	}
	m.gone.passes.Add(1)
	go func() {
		defer m.gone.passes.Done()
		if len(due) > 0 {
			defer func() {
				m.mu.Lock()
				m.gone.releasing = false
				m.mu.Unlock()
			}()
			m.releaseDue(due, host)
		}
		if finalize {
			m.collect()
		}
	}()
}

func (m *portableDelegatedResourceManager) finalizeDue(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.gone.lastFinalize.IsZero() && now.Sub(m.gone.lastFinalize) < trashFinalizeEvery {
		return false
	}
	m.gone.lastFinalize = now
	return true
}

func (m *portableDelegatedResourceManager) releaseDue(due []workerproc.Lease, host sessionHostIdentity) {
	records, err := workerproc.Processes(true)
	if err != nil {
		log.Printf("watcher: read processes before releasing gone Sessions: %v", err)
		return
	}
	for _, lease := range due {
		unit := lease.ResourceID
		if pane, ok := livePaneOf(records, lease, host); ok {
			log.Printf("watcher: kept lease %s: process %d is a live pane of the session host", unit, pane)
			m.forgetGone(unit)
			continue
		}
		m.mu.Lock()
		evidence := m.gone.absent[unit]
		m.mu.Unlock()
		if err := m.releaseUnit(unit, releaseReasonGone); err != nil {
			var inUse *scratchInUseError
			if errors.As(err, &inUse) {
				m.noteScratchKept(unit, inUse)
			} else {
				log.Printf("watcher: release lease %s of a gone Session: %v", unit, err)
			}
			continue
		}
		m.forgetGone(unit)
		log.Printf("watcher: released lease %s: its Session is on no session host inventory since %s (host pid %d)", unit, evidence.firstAbsent.UTC().Format(time.RFC3339), host.PID)
	}
}

func (m *portableDelegatedResourceManager) forgetGone(unit string) {
	m.mu.Lock()
	delete(m.gone.absent, unit)
	m.mu.Unlock()
}

// livePaneOf returns a process the lease owns that is a direct child of the
// session host: a pane. Its Session is not gone, whatever the inventory said.
func livePaneOf(records map[int]workerproc.Process, lease workerproc.Lease, host sessionHostIdentity) (int, bool) {
	if !host.known() {
		return 0, false
	}
	for pid, p := range workerproc.Owned(records, lease) {
		if p.PPID == host.PID {
			return pid, true
		}
	}
	return 0, false
}

// collect is the hourly pass of the collector: keeps past their date end,
// trash past its grace is deleted, and, where processes' use of paths can be
// read (Linux), unmarked t/ items are swept and the cache is brought under
// its budget.
func (m *portableDelegatedResourceManager) collect() {
	root, now := m.stateRoot(), m.clock()
	ended, err := gc.ExpireKeeps(root, now)
	if err != nil {
		log.Printf("watcher: keeps: %v", err)
	}
	for _, k := range ended {
		log.Printf("watcher: keep %s ended (%s, kept until %s)", k.ID, k.Path, k.Until.Format(time.RFC3339))
	}
	deleted, err := gc.Finalize(root, now)
	if err != nil {
		log.Printf("watcher: trash: %v", err)
	}
	for _, item := range deleted {
		log.Printf("watcher: trash: deleted %s (%s, trashed %s)", item.ID, item.Original, item.TrashedAt.Format(time.RFC3339))
	}
	if measured, err := gc.RecordSizes(root, now); err != nil {
		log.Printf("watcher: sizes: %v", err)
	} else if measured {
		log.Printf("watcher: measured today's sizes for the collector's ledger")
	}
	usage, ok := gc.ReadUsage()
	if !ok {
		return
	}
	swept, unproven, err := gc.SweepUnmarked(root, usage, now)
	if err != nil {
		log.Printf("watcher: t/ sweep: %v", err)
	} else {
		// Where reads leave no trace the sweep takes nothing it cannot prove
		// unread; what it found goes to review, and Brain is asked about it
		// (daemon/brain/t_review.go).
		gc.SyncReviews(root, unproven, now)
	}
	unread := make([]string, 0, len(unproven))
	for _, u := range unproven {
		unread = append(unread, u.Path)
	}
	for _, item := range swept {
		log.Printf("watcher: moved %s to the trash (%s): %s", item.Original, item.ID, item.Reason)
	}
	held := strings.Join(unread, ", ")
	m.mu.Lock()
	changed := held != m.gone.unread
	m.gone.unread = held
	m.mu.Unlock()
	if changed && held != "" {
		log.Printf("watcher: t/ is mounted noatime, so reads leave no trace: %d unmarked item(s) holding files are put to review, none taken: %s", len(unread), held)
	}
	evicted, err := gc.EvictCache(root, gc.CacheBudget, usage)
	if err != nil {
		log.Printf("watcher: cache: %v", err)
	}
	for _, e := range evicted {
		log.Printf("watcher: cache over its budget: deleted %s (%d MB, last changed %s)", e.Path, e.Bytes>>20, e.Changed.Format(time.RFC3339))
	}
}
