//go:build linux || darwin

package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/statedir"
	"github.com/daoleno/mewla/daemon/workerproc"
	"github.com/google/uuid"
)

const delegatedResourceReservationTTL = 2 * time.Minute

type portableDelegatedResourceManager struct {
	owner    string
	leaseDir string
	tempRoot string

	mu           sync.Mutex
	byTarget     map[string]string
	reserved     map[string]time.Time
	lastFullScan time.Time
	now          func() time.Time
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
		_ = m.removeOwnedTempDir(unit)
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

func (m *portableDelegatedResourceManager) UnitForTarget(target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byTarget[strings.TrimSpace(target)]
}

// Re-observation never kills: live legacy scopes and their processes survive
// daemon replacement. Only explicit Worker/Work cleanup calls Release.
func (m *portableDelegatedResourceManager) Reconcile(windows []tmuxPane) {
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
	if !validDelegatedResourceUnit(m.owner, unit) {
		return fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	if err := m.stopLease(unit); err != nil {
		return err
	}
	m.forgetUnit(unit)
	return nil
}

func (m *portableDelegatedResourceManager) stopLease(unit string) error {
	path, err := workerproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		return err
	}
	if err := workerproc.StopLease(path); err != nil {
		return err
	}
	return m.removeOwnedTempDir(unit)
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

func (m *portableDelegatedResourceManager) removeOwnedTempDir(unit string) error {
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
	if err := removeOwnedTree(tempDir); err != nil {
		return fmt.Errorf("remove delegated temporary directory: %w", err)
	}
	return nil
}

// removeOwnedTree deletes an already-validated owned temp tree. Nested tool
// caches (for example Go module dirs) may be mode 0555/000 with 0444 files;
// restore owner rwx on directories first so WalkDir can traverse and RemoveAll
// can unlink them.
func removeOwnedTree(root string) error {
	if err := makeOwnedTreeDirsOwnerAccessible(root); err != nil {
		return err
	}
	return os.RemoveAll(root)
}

// makeOwnedTreeDirsOwnerAccessible walks root without following symlinks and
// adds owner read+write+execute (mode|0700) only to real directories inside
// that tree. Owner-write alone is not enough: mode 000/0444 directories cannot
// be traversed until execute (and usually read) are restored. Callers must
// already have validated ownership of root; this never chmods sibling or
// foreign paths.
func makeOwnedTreeDirsOwnerAccessible(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode&0o700 == 0o700 {
			return nil
		}
		return os.Chmod(path, mode|0o700)
	})
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
