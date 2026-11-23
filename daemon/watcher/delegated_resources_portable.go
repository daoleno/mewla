//go:build linux || darwin

package watcher

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/statedir"
	"github.com/daoleno/mewla/daemon/userunit"
	"github.com/daoleno/mewla/daemon/workerproc"
	"github.com/google/uuid"
)

const delegatedResourceReservationTTL = 2 * time.Minute

// sessionSliceStopWait bounds the wait for a Session's slice to stop. A slow
// stop is not waited out: what still uses the scratch keeps it and the lease,
// and a later scan finishes the release.
const sessionSliceStopWait = 3 * time.Second

// stopSessionSlice stops the systemd slice of the Session with resource unit
// unit, and everything in it (Linux; package userunit). Tests replace it.
var stopSessionSlice = func(unit string) ([]string, error) {
	return userunit.StopSession(unit, sessionSliceStopWait)
}

type portableDelegatedResourceManager struct {
	owner    string
	leaseDir string
	tempRoot string

	mu           sync.Mutex
	byTarget     map[string]string
	reserved     map[string]time.Time
	lastFullScan time.Time
	now          func() time.Time

	// gone is the evidence that a lease's Session is gone (resource_gone.go).
	gone goneLeases
	// keptLogged is the last "kept scratch" line logged per unit.
	keptLogged map[string]string
}

func newPortableDelegatedResourceManager(owner string) (*portableDelegatedResourceManager, error) {
	owner = normalizeResourceOwner(owner)
	if owner == "" {
		return nil, fmt.Errorf("durable daemon identity is required")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home directory: %w", err)
	}
	leaseDir := filepath.Join(statedir.Default(home), "run", "worker-resources", owner)
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create delegated lease directory: %w", err)
	}
	// Short shared temp root keeps per-agent TMPDIR paths AF_UNIX-safe.
	tempRoot := filepath.Join(statedir.Default(home), "t")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create delegated temporary root: %w", err)
	}
	return &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}, nil
}

func (m *portableDelegatedResourceManager) Prepare(_ int) (*delegatedResourceSpec, error) {
	unit := delegatedResourceUnit(m.owner, uuid.NewString())
	m.mu.Lock()
	m.reserved[unit] = m.now().Add(delegatedResourceReservationTTL)
	m.mu.Unlock()
	tempDir, err := m.createOwnedTempDir(unit)
	if err != nil {
		m.forgetUnit(unit)
		return nil, err
	}
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err == nil {
		err = workerproc.WriteOwnershipLease(path, unit)
	}
	if err != nil {
		_ = m.releaseOwnedTempDir(unit, "")
		m.forgetUnit(unit)
		return nil, err
	}
	return &delegatedResourceSpec{Owner: m.owner, Unit: unit, TempDir: tempDir}, nil
}

func (m *portableDelegatedResourceManager) Bind(target, unit string) {
	target = strings.TrimSpace(target)
	unit = strings.TrimSpace(unit)
	if target == "" || !validDelegatedResourceUnit(m.owner, unit) {
		return
	}
	m.mu.Lock()
	delete(m.reserved, unit)
	m.byTarget[target] = unit
	m.mu.Unlock()
}

// OwnsUnit reports whether unit was issued by this daemon (its owner).
func (m *portableDelegatedResourceManager) OwnsUnit(unit string) bool {
	return validDelegatedResourceUnit(m.owner, unit)
}

func (m *portableDelegatedResourceManager) UnitForTarget(target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byTarget[strings.TrimSpace(target)]
}

// Re-observation never kills: live legacy scopes and their processes survive
// daemon replacement. Releases come from explicit Worker/Work cleanup and from
// releaseGone, which needs positive evidence that a Session is gone.
func (m *portableDelegatedResourceManager) Reconcile(windows []sessionPane) {
	m.mu.Lock()
	observe := m.lastFullScan.IsZero() || m.now().Sub(m.lastFullScan) >= 5*time.Second
	if observe {
		m.lastFullScan = m.now()
	}
	m.mu.Unlock()
	if observe {
		_ = workerproc.ObserveLeases(m.leaseDir)
	}

	for _, window := range windows {
		if window.delegated && validDelegatedResourceUnit(m.owner, window.resourceUnit) {
			m.Bind(window.target, window.resourceUnit)
		}
	}
}

func (m *portableDelegatedResourceManager) Release(target, unit string) error {
	target = strings.TrimSpace(target)
	unit = strings.TrimSpace(unit)
	if unit == "" && target != "" {
		unit = m.UnitForTarget(target)
	}
	if unit == "" {
		return nil
	}
	err := m.releaseUnit(unit, releaseReasonClosed)
	var inUse *scratchInUseError
	if errors.As(err, &inUse) {
		// The Session is closed; its scratch and lease stay until the process
		// using the scratch is gone, and the release of gone Sessions finishes.
		m.noteScratchKept(unit, inUse)
		m.forgetUnit(unit)
		return nil
	}
	return err
}

// Why a unit's scratch went to the trash.
const (
	releaseReasonClosed = "Session closed"
	releaseReasonGone   = "Session gone from the session host"
)

func (m *portableDelegatedResourceManager) releaseUnit(unit, reason string) error {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	if err := m.stopLease(unit, reason); err != nil {
		return err
	}
	m.forgetUnit(unit)
	m.mu.Lock()
	delete(m.keptLogged, unit)
	m.mu.Unlock()
	return nil
}

// stopLease stops what the unit owns, then moves its scratch to the trash,
// then removes the lease. In that order: a directory is never moved while a
// process may still write to it, and a crash at any step leaves the lease, so
// the next release finishes the rest.
func (m *portableDelegatedResourceManager) stopLease(unit, reason string) error {
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		return err
	}
	// First the Session's slice: it holds what the owned-process match cannot
	// find (setsid plus env -i, tmux panes, a nested Mewla's host).
	if slices, err := stopSessionSlice(unit); err != nil && !errors.Is(err, userunit.ErrUnsupported) {
		log.Printf("watcher: stop the slice of %s: %v", unit, err)
	} else if len(slices) > 0 {
		log.Printf("watcher: stopped %s", strings.Join(slices, ", "))
	}
	lease, err := workerproc.ReadLease(path)
	switch {
	case err == nil:
		if err := workerproc.StopOwned(lease, nil); err != nil {
			return err
		}
	case os.IsNotExist(err):
	default:
		return err
	}
	if reason != "" {
		m.moveKept(unit)
	}
	if err := m.releaseOwnedTempDir(unit, reason); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// moveKept moves what the Session kept (mewla gc keep) out of its scratch
// before the scratch goes.
func (m *portableDelegatedResourceManager) moveKept(unit string) {
	tempDir, err := m.resourceTempDir(unit)
	if err != nil {
		return
	}
	notes, err := gc.MoveKept(m.stateRoot(), unit, tempDir)
	if err != nil {
		log.Printf("watcher: move what %s kept: %v", unit, err)
	}
	for _, note := range notes {
		if note.Gone {
			log.Printf("watcher: keep %s dropped: %s was no longer there when its Session ended", note.Keep.ID, note.Keep.Path)
			continue
		}
		log.Printf("watcher: kept %s as %s until %s", note.Keep.Path, note.Keep.Moved, note.Keep.Until.Format(time.RFC3339))
	}
}

// ownsSessionToken reports whether token names one of this daemon's leases.
func (m *portableDelegatedResourceManager) ownsSessionToken(token string) bool {
	unit := delegatedResourceUnit(m.owner, token)
	if unit == "" {
		return false
	}
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// stateRoot is the state root the scratch root sits in; the trash is under it.
func (m *portableDelegatedResourceManager) stateRoot() string {
	return filepath.Dir(m.tempRoot)
}

func (m *portableDelegatedResourceManager) createOwnedTempDir(unit string) (string, error) {
	tempDir, err := m.resourceTempDir(unit)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(tempDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("delegated temporary directory collision for %s", unit)
		}
		return "", fmt.Errorf("create delegated temporary directory: %w", err)
	}
	markerPath := filepath.Join(tempDir, delegatedTempMarkerName)
	if err := os.WriteFile(markerPath, []byte(unit+"\n"), 0o600); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("write delegated temporary ownership marker: %w", err)
	}
	if err := os.Chmod(markerPath, 0o600); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("chmod delegated temporary ownership marker: %w", err)
	}
	return tempDir, nil
}

// releaseOwnedTempDir moves the unit's scratch to the trash with reason, or
// deletes it when reason is "" (a scratch made a moment ago for a spawn that
// failed holds nothing).
func (m *portableDelegatedResourceManager) releaseOwnedTempDir(unit, reason string) error {
	tempDir, err := m.resourceTempDir(unit)
	if err != nil {
		return err
	}
	markedUnit, ok := readDelegatedTempMarker(tempDir)
	if !ok || markedUnit != unit {
		// Foreign or corrupt short entries remain untouched.
		return nil
	}
	info, err := os.Lstat(tempDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat delegated temporary directory: %w", err)
	}
	// Never follow or mutate a symlink-replaced root.
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	// Re-check the marker after Lstat so a swapped foreign root stays untouched.
	markedUnit, ok = readDelegatedTempMarker(tempDir)
	if !ok || markedUnit != unit {
		return nil
	}
	if reason == "" {
		if err := gc.RemoveTree(tempDir); err != nil {
			return fmt.Errorf("remove delegated temporary directory: %w", err)
		}
		return nil
	}
	// A process still running from the scratch (its exe or its cwd inside)
	// keeps it, and the lease with it: moving the directory would take the
	// evidence of whose that process is (PR 2 stops such processes).
	if user, ok := scratchUser(tempDir); ok {
		return &scratchInUseError{dir: tempDir, user: user}
	}
	if _, err := gc.Trash(m.stateRoot(), gc.KindScratch, tempDir, unit, reason, m.clock()); err != nil {
		return fmt.Errorf("move delegated temporary directory to the trash: %w", err)
	}
	return nil
}

// scratchInUseError says a unit's scratch was kept because a process still
// uses it. The lease stays, so a later release tries again.
type scratchInUseError struct {
	dir  string
	user scratchUsage
}

func (e *scratchInUseError) Error() string {
	return fmt.Sprintf("kept scratch %s: process %d (%s) is still using it", e.dir, e.user.PID, e.user.Exe)
}

// noteScratchKept logs a kept scratch once per unit and user, not on every
// retry.
func (m *portableDelegatedResourceManager) noteScratchKept(unit string, err *scratchInUseError) {
	line := err.Error()
	m.mu.Lock()
	if m.keptLogged == nil {
		m.keptLogged = map[string]string{}
	}
	same := m.keptLogged[unit] == line
	m.keptLogged[unit] = line
	m.mu.Unlock()
	if !same {
		log.Printf("watcher: %s", line)
	}
}

func (m *portableDelegatedResourceManager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func readDelegatedTempMarker(tempDir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(tempDir, delegatedTempMarkerName))
	if err != nil {
		return "", false
	}
	unit := strings.TrimSpace(string(raw))
	if unit == "" {
		return "", false
	}
	return unit, true
}

func (m *portableDelegatedResourceManager) resourceTempDir(unit string) (string, error) {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return "", fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	if strings.TrimSpace(m.tempRoot) == "" {
		return "", fmt.Errorf("delegated temporary root is unavailable")
	}
	digest := shortDelegatedTempDigest(unit)
	if digest == "" {
		return "", fmt.Errorf("delegated temporary digest unavailable")
	}
	return filepath.Join(m.tempRoot, digest), nil
}

func (m *portableDelegatedResourceManager) forgetUnit(unit string) {
	m.mu.Lock()
	delete(m.reserved, unit)
	for target, bound := range m.byTarget {
		if bound == unit {
			delete(m.byTarget, target)
		}
	}
	m.mu.Unlock()
}

func (m *portableDelegatedResourceManager) CaptureOwnership(unit string) error {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return fmt.Errorf("refuse unowned resource token")
	}
	if _, err := workerproc.Processes(true); err != nil {
		return err
	}
	return workerproc.ObserveLeases(m.leaseDir)
}

// ReleaseProcess stops an explicitly selected owned tool tree while retaining
// the provider and pane. The caller supplies the PID start identity from telemetry.
func (w *Watcher) ReleaseProcess(sessionID string, pid int, start string) error {
	worker := w.GetWorker(sessionID)
	if worker == nil || !worker.Delegated || worker.Hidden {
		return fmt.Errorf("a live delegated Worker is required")
	}
	identity, known := w.targetForSession(sessionID)
	if !known || identity.ProcessID <= 1 {
		return fmt.Errorf("Worker process identity unavailable")
	}
	manager, ok := w.resourceManager().(*portableDelegatedResourceManager)
	if !ok {
		return fmt.Errorf("process ownership unavailable")
	}
	unit := manager.UnitForTarget(sessionID)
	if !validDelegatedResourceUnit(manager.owner, unit) {
		return fmt.Errorf("Worker ownership unavailable")
	}
	path, err := workerproc.LeasePath(manager.leaseDir, unit)
	if err != nil {
		return err
	}
	lease, err := workerproc.ReadLease(path)
	if err != nil {
		return err
	}
	records, err := workerproc.Processes(true)
	if err != nil {
		return err
	}
	target, ok := workerproc.Owned(records, lease)[pid]
	if !ok || start == "" || target.Start != start {
		return fmt.Errorf("selected process is absent, reused, or not owned by this Worker")
	}
	for parent := identity.ProcessID; parent > 1; {
		if parent == pid {
			return fmt.Errorf("cannot release the Worker provider or its ancestors")
		}
		p, ok := records[parent]
		if !ok || p.PPID == parent {
			break
		}
		parent = p.PPID
	}
	if err := guardTargetIdentity(w.targetForSession, sessionID, identity); err != nil {
		return err
	}
	// A new selection lease has no inherited token: only the selected exact
	// process and its descendants are eligible, never sibling workloads.
	return workerproc.StopOwned(workerproc.Lease{Observed: []workerproc.ProcessIdentity{{PID: pid, Start: start}}}, nil)
}

// Reuse returns an existing unit's spec, making its TMPDIR and lease again if
// either is gone.
func (m *portableDelegatedResourceManager) Reuse(unit string) (*delegatedResourceSpec, error) {
	unit = strings.TrimSpace(unit)
	tempDir, err := m.resourceTempDir(unit)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(tempDir); os.IsNotExist(statErr) {
		if tempDir, err = m.createOwnedTempDir(unit); err != nil {
			return nil, err
		}
	}
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := workerproc.WriteOwnershipLease(path, unit); err != nil {
			return nil, err
		}
	}
	return &delegatedResourceSpec{Owner: m.owner, Unit: unit, TempDir: tempDir}, nil
}

// Survivors lists the processes the unit still owns, oldest first, without
// touching them. Mewla's own helpers (its tool servers an agent started) are
// not listed: they belonged to the dead agent and say nothing about its work.
func (m *portableDelegatedResourceManager) Survivors(unit string) ([]agentSurvivor, error) {
	unit = strings.TrimSpace(unit)
	if !validDelegatedResourceUnit(m.owner, unit) {
		return nil, fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		return nil, err
	}
	lease, err := workerproc.ReadLease(path)
	if os.IsNotExist(err) {
		lease = workerproc.Lease{Version: 1, ResourceID: unit}
	} else if err != nil {
		return nil, err
	}
	records, err := workerproc.Processes(true)
	if err != nil {
		return nil, err
	}
	self := filepath.Base(selfExecutable())
	var out []agentSurvivor
	for pid, process := range workerproc.Owned(records, lease) {
		args := workerproc.Args(pid)
		if len(args) == 0 {
			args = strings.Fields(process.Args)
		}
		if len(args) == 0 || (self != "" && filepath.Base(args[0]) == self) {
			continue
		}
		started, _ := workerproc.StartTime(pid)
		out = append(out, agentSurvivor{PID: pid, Command: survivorCommand(args), Started: started, args: args, ppid: process.PPID})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Started.Equal(out[j].Started) {
			return out[i].Started.Before(out[j].Started)
		}
		return out[i].PID < out[j].PID
	})
	return out, nil
}

func selfExecutable() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}
