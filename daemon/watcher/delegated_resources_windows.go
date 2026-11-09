//go:build windows

package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daoleno/mewla/daemon/statedir"
	"github.com/google/uuid"
)

// On Windows every Session's processes run in the session host, inside one
// Job Object per Session that ends them all when the Session is killed; only
// a process started with an explicit request to break away (as a detached
// mewla daemon is) leaves it. So a delegated Worker needs no process leases:
// its unit names its Session and owns a temporary directory, which Release
// removes after the Session's cleanup has killed it. A process that broke
// away is not followed, as the Unix lease scan would.
type jobDelegatedResourceManager struct {
	owner    string
	tempRoot string

	mu       sync.Mutex
	byTarget map[string]string
}

func newDelegatedResourceManager(owner string) delegatedResourceManager {
	owner = normalizeResourceOwner(owner)
	if owner == "" {
		return unavailableDelegatedResourceManager{reason: "durable daemon identity is required"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return unavailableDelegatedResourceManager{reason: fmt.Sprintf("locate home directory: %v", err)}
	}
	tempRoot := filepath.Join(statedir.Default(home), "t")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return unavailableDelegatedResourceManager{reason: fmt.Sprintf("create delegated temporary root: %v", err)}
	}
	return &jobDelegatedResourceManager{owner: owner, tempRoot: tempRoot, byTarget: map[string]string{}}
}

func validateDelegatedWorkspace(cwd string) error {
	_, err := validateDelegatedWorkspacePath(cwd)
	return err
}

func (m *jobDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	unit := delegatedResourceUnit(m.owner, uuid.NewString())
	dir, err := m.tempDir(unit)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create delegated temporary directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, delegatedTempMarkerName), []byte(unit+"\n"), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write delegated temporary ownership marker: %w", err)
	}
	return &delegatedResourceSpec{Owner: m.owner, Unit: unit, TempDir: dir}, nil
}

func (m *jobDelegatedResourceManager) Bind(target, unit string) {
	target, unit = strings.TrimSpace(target), strings.TrimSpace(unit)
	if target == "" || !validDelegatedResourceUnit(m.owner, unit) {
		return
	}
	m.mu.Lock()
	m.byTarget[target] = unit
	m.mu.Unlock()
}

func (m *jobDelegatedResourceManager) UnitForTarget(target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byTarget[strings.TrimSpace(target)]
}

func (m *jobDelegatedResourceManager) Reconcile(windows []sessionPane) {
	for _, window := range windows {
		if window.delegated && validDelegatedResourceUnit(m.owner, window.resourceUnit) {
			m.Bind(window.target, window.resourceUnit)
		}
	}
}

// Release removes the unit's temporary directory; the Session's own Job
// ended its processes. A directory without this unit's marker is not ours.
func (m *jobDelegatedResourceManager) Release(target, unit string) error {
	target, unit = strings.TrimSpace(target), strings.TrimSpace(unit)
	if unit == "" && target != "" {
		unit = m.UnitForTarget(target)
	}
	if unit == "" {
		return nil
	}
	dir, err := m.tempDir(unit)
	if err != nil {
		return err
	}
	if raw, err := os.ReadFile(filepath.Join(dir, delegatedTempMarkerName)); err == nil && strings.TrimSpace(string(raw)) == unit {
		if info, err := os.Lstat(dir); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("remove delegated temporary directory: %w", err)
			}
		}
	}
	m.mu.Lock()
	for t, u := range m.byTarget {
		if u == unit {
			delete(m.byTarget, t)
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *jobDelegatedResourceManager) tempDir(unit string) (string, error) {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return "", fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	digest := shortDelegatedTempDigest(unit)
	if digest == "" {
		return "", fmt.Errorf("delegated temporary digest unavailable")
	}
	return filepath.Join(m.tempRoot, digest), nil
}
