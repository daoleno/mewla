package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeDelegatedResourceManager struct {
	spec        *delegatedResourceSpec
	prepareErr  error
	boundTarget string
	boundUnit   string
	released    []string
	releaseErr  error
}

func (m *fakeDelegatedResourceManager) Prepare(int) (*delegatedResourceSpec, error) {
	return m.spec, m.prepareErr
}

func (m *fakeDelegatedResourceManager) Bind(target, unit string) {
	m.boundTarget = target
	m.boundUnit = unit
}

func (m *fakeDelegatedResourceManager) UnitForTarget(target string) string {
	if target == m.boundTarget {
		return m.boundUnit
	}
	return ""
}

func (*fakeDelegatedResourceManager) Reconcile([]tmuxPane) {}

func (m *fakeDelegatedResourceManager) Release(target, unit string) error {
	m.released = append(m.released, target+"\t"+unit)
	return m.releaseErr
}

func newTestPortableResourceManager(t *testing.T, owner string) *portableDelegatedResourceManager {
	t.Helper()
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	for _, dir := range []string{leaseDir, tempRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}
}

func TestDelegatedResourceUnitIsStrictlyNamespaced(t *testing.T) {
	unit := delegatedResourceUnit("Daemon-ID-ABCDEF", "01234567-89ab-cdef-0123-456789abcdef")
	if unit != "zen-worker-daemonidabcdef-0123456789abcdef0123456789abcdef.scope" {
		t.Fatalf("unit = %q", unit)
	}
	if !validDelegatedResourceUnit("daemon-id-abcdef", unit) {
		t.Fatalf("expected %q to belong to the daemon", unit)
	}
	for _, candidate := range []string{
		"tmux-spawn-01234567.scope",
		"zen-worker-otherdaemon-0123456789abcdef0123456789abcdef.scope",
		"zen-worker-daemonidabcdef-not-a-uuid.scope",
		"zen-worker-daemonidabcdef-0123456789abcdef0123456789abcdef.service",
	} {
		if validDelegatedResourceUnit("daemon-id-abcdef", candidate) {
			t.Fatalf("accepted unowned or malformed unit %q", candidate)
		}
	}
}

func TestCloneEnvironmentDoesNotMutateCaller(t *testing.T) {
	original := map[string]string{"KEEP": "value"}
	before := os.Getenv(delegatedMarkerEnv)
	copy := cloneEnvironment(original)
	copy[delegatedMarkerEnv] = "1"
	if _, exists := original[delegatedMarkerEnv]; exists {
		t.Fatal("cloneEnvironment mutated the caller map")
	}
	if os.Getenv(delegatedMarkerEnv) != before {
		t.Fatal("cloneEnvironment mutated the process environment")
	}
}

func TestUnavailableResourceManagerFailsClosed(t *testing.T) {
	manager := unavailableDelegatedResourceManager{reason: "no user manager"}
	if _, err := manager.Prepare(0); err == nil || !strings.Contains(err.Error(), "ownership tracking unavailable") {
		t.Fatalf("Prepare error = %v", err)
	}
}

func TestDelegatedWorkspacePathRejectsVolatileRoots(t *testing.T) {
	for _, path := range []string{
		"/tmp/zen-worktree",
		"/private/tmp/zen-worktree",
		"/var/tmp/zen-worktree",
		"/dev/shm/zen-worktree",
		"/run/user/501/zen-worktree",
	} {
		if _, err := validateDelegatedWorkspacePath(path); err == nil || !strings.Contains(err.Error(), "volatile or memory-backed temporary storage") {
			t.Fatalf("validateDelegatedWorkspacePath(%q) error = %v", path, err)
		}
	}
}

func TestDelegatedWorkspacePathAcceptsDurableHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".zen", "worktrees", "zen", "task")
	resolved, err := validateDelegatedWorkspacePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == "" {
		t.Fatal("expected resolved durable path")
	}
}

func TestCreateDelegatedSessionPassesOwnedResourceToTmux(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$MEWLA_TEST_TMUX_LOG"
case "$1" in
  new-session) printf '%%1\n' ;;
esac
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MEWLA_TEST_TMUX_LOG", logPath)

	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{spec: &delegatedResourceSpec{
		Owner:   "abc123",
		Unit:    unit,
		TempDir: filepath.Join(dir, "owned-tmp"),
	}}
	w := New(0)
	w.resources = manager
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	callerEnv := map[string]string{"KEEP": "yes"}
	target, err := w.CreateSession("", CreateSessionOptions{
		Cwd:       cwd,
		Command:   "codex",
		Name:      "test",
		Detached:  true,
		Delegated: true,
		Env:       callerEnv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "%1" || manager.boundTarget != target || manager.boundUnit != unit {
		t.Fatalf("target/binding = %q %q %q", target, manager.boundTarget, manager.boundUnit)
	}
	if _, exists := callerEnv[delegatedMarkerEnv]; exists {
		t.Fatal("CreateSession mutated caller Env")
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	for _, want := range []string{
		delegatedMarkerEnv + "=1",
		delegatedResourceUnitEnv + "=" + unit,
		"TMPDIR=" + filepath.Join(dir, "owned-tmp"),
		"MEWLA_BUILD_TMPDIR=" + filepath.Join(dir, "owned-tmp"),
		"set-option -p -t " + target + " @zen_worker_delegated 1",
		"set-option -p -t " + target + " @zen_worker_resource_unit " + unit,
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("tmux calls missing %q:\n%s", want, calls)
		}
	}
}

func TestCreateDelegatedSessionRollsBackWhenOwnershipMarkersFail(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$MEWLA_TEST_TMUX_LOG"
case "$1" in
  new-session) printf '%%7\n' ;;
  set-option)
    case "$*" in
      *@zen_worker_resource_unit*) exit 1 ;;
    esac
    ;;
esac
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MEWLA_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{spec: &delegatedResourceSpec{
		Owner:   "abc123",
		Unit:    unit,
		TempDir: filepath.Join(dir, "owned-tmp"),
	}}
	w := New(0)
	w.resources = manager
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateSession("", CreateSessionOptions{
		Cwd:       cwd,
		Command:   "codex",
		Name:      "test",
		Detached:  true,
		Delegated: true,
	}); err == nil || !strings.Contains(err.Error(), "mark owned tmux pane") {
		t.Fatalf("CreateSession error = %v", err)
	}
	if len(manager.released) != 1 || manager.released[0] != "\t"+unit {
		t.Fatalf("released = %#v", manager.released)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "kill-pane -t %7") {
		t.Fatalf("unmarked window was not rolled back:\n%s", raw)
	}
}

func TestPortableResourceReleaseRemovesOnlyOwnedTemporaryDirectory(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}
	ownedTemp, err := manager.createOwnedTempDir(unit)
	if err != nil {
		t.Fatal(err)
	}
	foreignTemp := filepath.Join(tempRoot, "user-data")
	if err := os.MkdirAll(foreignTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownedTemp); !os.IsNotExist(err) {
		t.Fatalf("owned temporary directory still exists: %v", err)
	}
	if _, err := os.Stat(foreignTemp); err != nil {
		t.Fatalf("foreign directory was touched: %v", err)
	}
}

func TestPortableResourceTempDirCollisionFailsClosed(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	tempRoot := filepath.Join(root, "t")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		tempRoot: tempRoot,
	}
	collision := filepath.Join(tempRoot, shortDelegatedTempDigest(unit))
	if err := os.Mkdir(collision, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.createOwnedTempDir(unit); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("createOwnedTempDir error = %v", err)
	}
}

func TestPortableResourceReleaseLeavesForeignOrCorruptTempUntouched(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	for _, dir := range []string{leaseDir, tempRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}
	tempDir := filepath.Join(tempRoot, shortDelegatedTempDigest(unit))
	if err := os.Mkdir(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Corrupt marker: wrong unit content.
	if err := os.WriteFile(filepath.Join(tempDir, delegatedTempMarkerName), []byte("not-the-unit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(tempDir, "cache")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "NOTICE"), []byte("keep\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = makeOwnedTreeDirsOwnerAccessible(tempDir)
	})
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("corrupt temp directory was removed: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("foreign/corrupt nested dir was mutated: info=%v err=%v", info, err)
	}
}

func TestPortableResourceReleaseRemovesShortRestrictedNestedTrees(t *testing.T) {
	cases := []struct {
		name  string
		seed  func(t *testing.T, ownedTemp, foreign string)
		check func(t *testing.T, ownedTemp, foreign string)
	}{
		{
			name: "readonly-cache",
			seed: func(t *testing.T, ownedTemp, foreign string) {
				writeReadonlyNestedCache(t, foreign)
				writeReadonlyNestedCache(t, ownedTemp)
				if err := os.Symlink(foreign, filepath.Join(ownedTemp, "escape")); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, ownedTemp, foreign string) {
				if info, err := os.Stat(filepath.Join(foreign, "pkg", "mod", "cache")); err != nil || info.Mode().Perm()&0o200 != 0 {
					t.Fatalf("foreign tree reached through nested symlink was mutated: info=%v err=%v", info, err)
				}
			},
		},
		{
			name: "mode-zero",
			seed: func(t *testing.T, ownedTemp, _ string) {
				writeModeZeroNestedDir(t, ownedTemp)
			},
			check: func(t *testing.T, _, _ string) {},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := "abc123"
			unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
			manager := newTestPortableResourceManager(t, owner)
			foreign := filepath.Join(t.TempDir(), "foreign")
			if err := os.MkdirAll(foreign, 0o700); err != nil {
				t.Fatal(err)
			}
			ownedTemp, err := manager.createOwnedTempDir(unit)
			if err != nil {
				t.Fatal(err)
			}
			tc.seed(t, ownedTemp, foreign)
			if err := manager.Release("", unit); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(ownedTemp); !os.IsNotExist(err) {
				t.Fatalf("owned short temp still exists: %v", err)
			}
			tc.check(t, ownedTemp, foreign)
		})
	}
}

func TestPortableResourceReleaseDoesNotFollowSymlinkOrForeignMarkerRoot(t *testing.T) {
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	legacyRoot := filepath.Join(root, "tmp", "worker-resources", owner)
	foreign := filepath.Join(root, "foreign")
	for _, dir := range []string{leaseDir, tempRoot, legacyRoot, foreign} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeReadonlyNestedCache(t, foreign)
	foreignNotice := filepath.Join(foreign, "pkg", "mod", "cache", "NOTICE")

	// Short path is a symlink to foreign content; marker check must fail closed.
	shortTemp := filepath.Join(tempRoot, shortDelegatedTempDigest(unit))
	if err := os.Symlink(foreign, shortTemp); err != nil {
		t.Fatal(err)
	}
	// Legacy path is a symlink to the same foreign tree.
	legacyTemp := filepath.Join(legacyRoot, unit)
	if err := os.Symlink(foreign, legacyTemp); err != nil {
		t.Fatal(err)
	}

	manager := &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}
	if err := manager.Release("", unit); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(shortTemp); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("short symlink root was removed or replaced: info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(legacyTemp); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy symlink root was removed or replaced: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Join(foreign, "pkg", "mod", "cache")); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("foreign nested dir was mutated through symlink: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(foreignNotice); err != nil {
		t.Fatalf("foreign NOTICE was removed through symlink: %v", err)
	}
}

func writeReadonlyNestedCache(t *testing.T, root string) {
	t.Helper()
	cacheDir := filepath.Join(root, "pkg", "mod", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notice := filepath.Join(cacheDir, "NOTICE")
	if err := os.WriteFile(notice, []byte("go module cache\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cacheDir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "pkg", "mod"), 0o555); err != nil {
		t.Fatal(err)
	}
	// TempDir teardown uses RemoveAll; restore owner rwx if the tree survives the test.
	t.Cleanup(func() {
		_ = makeOwnedTreeDirsOwnerAccessible(root)
	})
}

func writeModeZeroNestedDir(t *testing.T, root string) {
	t.Helper()
	sealed := filepath.Join(root, "sealed")
	inner := filepath.Join(sealed, "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "blob"), []byte("opaque\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(inner, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = makeOwnedTreeDirsOwnerAccessible(root)
	})
}

func TestPortableResourceReconcilePreservesOrphanUntilExplicitCleanup(t *testing.T) {
	owner := "abc123"
	orphan := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	live := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	for _, dir := range []string{leaseDir, tempRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}
	orphanTemp, err := manager.createOwnedTempDir(orphan)
	if err != nil {
		t.Fatal(err)
	}
	liveTemp, err := manager.createOwnedTempDir(live)
	if err != nil {
		t.Fatal(err)
	}
	foreignTemp := filepath.Join(tempRoot, "abcdef")
	if err := os.Mkdir(foreignTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	manager.Reconcile([]tmuxPane{{
		target:       "%1",
		delegated:    true,
		resourceUnit: live,
	}})
	if _, err := os.Stat(orphanTemp); err != nil {
		t.Fatalf("orphan temp still exists: %v", err)
	}
	if _, err := os.Stat(liveTemp); err != nil {
		t.Fatalf("live temp was removed: %v", err)
	}
	if _, err := os.Stat(foreignTemp); err != nil {
		t.Fatalf("foreign temp was touched: %v", err)
	}
}

func TestPortableResourceReconcileLeavesRetiredLayoutUntouched(t *testing.T) {
	owner := "abc123"
	orphan := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	live := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	tempRoot := filepath.Join(root, "t")
	legacyRoot := filepath.Join(root, "tmp", "worker-resources", owner)
	for _, dir := range []string{leaseDir, tempRoot, legacyRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	orphanLegacy := filepath.Join(legacyRoot, orphan)
	liveLegacy := filepath.Join(legacyRoot, live)
	foreignLegacy := filepath.Join(legacyRoot, "not-a-unit")
	for _, dir := range []string{orphanLegacy, liveLegacy, foreignLegacy} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: leaseDir,
		tempRoot: tempRoot,
		byTarget: make(map[string]string),
		reserved: make(map[string]time.Time),
		now:      time.Now,
	}
	manager.Reconcile([]tmuxPane{{
		target:       "%1",
		delegated:    true,
		resourceUnit: live,
	}})
	if _, err := os.Stat(orphanLegacy); err != nil {
		t.Fatalf("retired layout was modified: %v", err)
	}
	if _, err := os.Stat(liveLegacy); err != nil {
		t.Fatalf("live legacy temp was removed: %v", err)
	}
	if _, err := os.Stat(foreignLegacy); err != nil {
		t.Fatalf("foreign legacy directory was touched: %v", err)
	}
}

func TestShortDelegatedTempDigestIsStableURLSafe(t *testing.T) {
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	first := shortDelegatedTempDigest(unit)
	second := shortDelegatedTempDigest(unit)
	if first != second || len(first) != delegatedTempDigestLen {
		t.Fatalf("digest = %q / %q", first, second)
	}
	for _, char := range first {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		t.Fatalf("digest %q is not URL-safe", first)
	}
}

func TestKillDelegatedSessionReleasesExactBoundUnit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$MEWLA_TEST_TMUX_LOG"
target=
prev=
for arg in "$@"; do
  if [ "$prev" = "-t" ]; then target=$arg; fi
  prev=$arg
done
if [ "$1" = "list-panes" ]; then
  echo "%42"
  exit 0
fi
if [ "$1" = "show-options" ]; then
  echo 1
fi
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MEWLA_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{boundTarget: "%42", boundUnit: unit}
	w := New(0)
	w.resources = manager
	if err := w.KillSession("%42"); err != nil {
		t.Fatal(err)
	}
	if len(manager.released) != 1 || manager.released[0] != "%42\t"+unit {
		t.Fatalf("released = %#v", manager.released)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "kill-pane -t %42") {
		t.Fatalf("tmux calls:\n%s", raw)
	}
}
