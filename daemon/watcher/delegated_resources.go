package watcher

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	delegatedMarkerEnv        = "MEWLA_WORKER_DELEGATED"
	delegatedResourceUnitEnv  = "MEWLA_WORKER_RESOURCE_UNIT"
	delegatedResourceOwnerEnv = "MEWLA_WORKER_RESOURCE_OWNER"

	// Short durable temp dirs keep AF_UNIX paths under sockaddr sun_path limits.
	delegatedTempMarkerName = ".mewla-worker-unit"
	delegatedTempDigestLen  = 6
)

var (
	resourceOwnerSanitizer = regexp.MustCompile(`[^a-z0-9]+`)
)

// Resource IDs are ownership tokens, including legacy .scope-shaped tokens.
// No cgroup or resource budget is created by this manager.
type delegatedResourceSpec struct{ Owner, Unit, TempDir string }

type delegatedResourceManager interface {
	Prepare(activeSessions int) (*delegatedResourceSpec, error)
	Bind(target, unit string)
	UnitForTarget(target string) string
	Reconcile(windows []tmuxPane)
	Release(target, unit string) error
}

type noopDelegatedResourceManager struct{}

func (noopDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	return nil, nil
}

func (noopDelegatedResourceManager) Bind(string, string) {}

func (noopDelegatedResourceManager) UnitForTarget(string) string { return "" }

func (noopDelegatedResourceManager) Reconcile([]tmuxPane) {}

func (noopDelegatedResourceManager) Release(string, string) error { return nil }

type unavailableDelegatedResourceManager struct{ reason string }

func (m unavailableDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	return nil, fmt.Errorf("delegated ownership tracking unavailable: %s", strings.TrimSpace(m.reason))
}

func (unavailableDelegatedResourceManager) Bind(string, string) {}

func (unavailableDelegatedResourceManager) UnitForTarget(string) string { return "" }

func (unavailableDelegatedResourceManager) Reconcile([]tmuxPane) {}

func (unavailableDelegatedResourceManager) Release(string, string) error { return nil }

func normalizeResourceOwner(owner string) string {
	owner = strings.ToLower(strings.TrimSpace(owner))
	owner = resourceOwnerSanitizer.ReplaceAllString(owner, "")
	// Daemon IDs are UUID-like. Preserve their full 128-bit normalized value so
	// two state directories cannot share a kill namespace merely because their
	// IDs have the same short prefix; the resulting systemd unit is still far
	// below the platform name limit.
	if len(owner) > 32 {
		owner = owner[:32]
	}
	return owner
}

func delegatedResourceUnit(owner, token string) string {
	owner = normalizeResourceOwner(owner)
	token = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(token), "-", ""))
	if owner == "" || len(token) != 32 {
		return ""
	}
	for _, char := range token {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return "mewla-worker-" + owner + "-" + token + ".scope"
}

func shortDelegatedTempDigest(unit string) string {
	sum := sha256.Sum256([]byte(unit))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(encoded) < delegatedTempDigestLen {
		return encoded
	}
	return encoded[:delegatedTempDigestLen]
}

func validDelegatedResourceUnit(owner, unit string) bool {
	owner = normalizeResourceOwner(owner)
	unit = strings.TrimSpace(unit)
	prefix := "mewla-worker-" + owner + "-"
	if owner == "" || !strings.HasPrefix(unit, prefix) || !strings.HasSuffix(unit, ".scope") {
		return false
	}
	token := strings.TrimSuffix(strings.TrimPrefix(unit, prefix), ".scope")
	return delegatedResourceUnit(owner, token) == unit
}

func shellExecCommand(args []string) string {
	quoted := make([]string, 0, len(args)+1)
	quoted = append(quoted, "exec")
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func cloneEnvironment(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values)+3)
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func validateDelegatedWorkspacePath(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve delegated workspace %q: %w", cwd, err)
	}
	resolved := absolute
	if value, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		resolved = value
	}

	roots := []string{
		os.TempDir(),
		"/tmp",
		"/private/tmp",
		"/var/tmp",
		"/private/var/tmp",
		"/dev/shm",
		"/run/user",
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rootAbsolute, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		rootResolved := rootAbsolute
		if value, resolveErr := filepath.EvalSymlinks(rootAbsolute); resolveErr == nil {
			rootResolved = value
		}
		if pathWithinRoot(absolute, rootAbsolute) || pathWithinRoot(resolved, rootResolved) {
			return "", fmt.Errorf("delegated Mewla Worker cwd %q is on volatile or memory-backed temporary storage; use a durable workspace such as $MEWLA_WORKTREE_ROOT (default ~/.mewla/worktrees)", cwd)
		}
	}
	return resolved, nil
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
