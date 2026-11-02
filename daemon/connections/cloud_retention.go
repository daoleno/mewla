package connections

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A short shared gate covers resolving a launcher and acquiring its generation
// lease. Activation/pruning hold the exclusive gate, closing the resolve/delete
// race. Per-generation shared leases live until commands (including children)
// exit; the pruner only removes generations it can lock exclusively.
func cloudGeneration(binary string) string {
	for path := filepath.Dir(binary); path != filepath.Dir(path); path = filepath.Dir(path) {
		candidate := filepath.Base(filepath.Dir(path)) == "versions" || filepath.Base(path) == "google-cloud-sdk" && filepath.Base(filepath.Dir(path)) == "gcp"
		if candidate {
			if _, err := os.Stat(filepath.Join(cloudGenerationRoot(path), ".mewla-managed")); err == nil {
				return path
			}
		}
	}
	return ""
}

// Only explicitly owned state directories get a marker. A PATH executable may
// itself live under a directory named versions; that must not mutate its install.
func markCloudManagedRoot(root string) error {
	file, err := os.OpenFile(filepath.Join(root, ".mewla-managed"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return file.Close()
}
func cloudGenerationRoot(generation string) string {
	parent := filepath.Dir(generation)
	if filepath.Base(parent) == "versions" {
		return filepath.Dir(parent)
	}
	return parent
}
func closeCloudLease(lease *os.File) {
	if lease != nil {
		_ = lease.Close()
	}
}

func holdCloudBinary(binary string) (string, *os.File, error) {
	if !strings.ContainsRune(binary, filepath.Separator) {
		return binary, nil, nil
	}
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return "", nil, err
	}
	generation := cloudGeneration(resolved)
	if generation == "" {
		return resolved, nil, nil
	}
	root := cloudGenerationRoot(generation)
	gate, err := lockCloudFile(filepath.Join(root, "generations.lock"), true, false)
	if err != nil {
		return "", nil, err
	}
	defer gate.Close()
	// Resolve again while activation is excluded. A launcher may have changed
	// between the first read (used to locate its lock) and acquiring the gate.
	resolved, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return "", nil, err
	}
	generation = cloudGeneration(resolved)
	if generation == "" || cloudGenerationRoot(generation) != root {
		return "", nil, errors.New("Cloud CLI installation changed; retry")
	}
	lease, err := lockCloudFile(filepath.Join(generation, ".lease"), true, false)
	return resolved, lease, err
}
func newCloudGeneration(root, prefix string) (string, *os.File, error) {
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0700); err != nil {
		return "", nil, err
	}
	if err := markCloudManagedRoot(root); err != nil {
		return "", nil, err
	}
	gate, err := lockCloudFile(filepath.Join(root, "generations.lock"), false, false)
	if err != nil {
		return "", nil, err
	}
	defer gate.Close()
	dir, err := os.MkdirTemp(filepath.Join(root, "versions"), prefix)
	if err != nil {
		return "", nil, err
	}
	lease, err := lockCloudFile(filepath.Join(dir, ".lease"), true, false)
	if err != nil {
		_ = os.RemoveAll(dir)
	}
	return dir, lease, err
}

func activateCloudGeneration(root, binary, installed string) error {
	gate, err := lockCloudFile(filepath.Join(root, "generations.lock"), false, false)
	if err != nil {
		return err
	}
	defer gate.Close()
	launcher := filepath.Join(root, "bin", binary)
	old, _ := filepath.EvalSymlinks(launcher)
	previous := cloudGeneration(old)
	if err = os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
		return err
	}
	// The rollback pointer belongs to the candidate. Publishing the launcher
	// atomically publishes both its executable and its predecessor; a failed
	// activation cannot change the active generation's rollback owner.
	generation := cloudGeneration(installed)
	if generation == "" || cloudGenerationRoot(generation) != root {
		return errors.New("Invalid managed generation")
	}
	if previous != "" && previous != generation && cloudGenerationRoot(previous) == root {
		if err = os.Symlink(previous, filepath.Join(generation, "previous")); err != nil {
			return err
		}
	}
	if err = os.WriteFile(filepath.Join(generation, ".activated"), nil, 0600); err != nil {
		return err
	}
	link := filepath.Join(root, ".activate-new")
	_ = os.Remove(link)
	if err = os.Symlink(installed, link); err != nil {
		return err
	}
	defer os.Remove(link)
	return os.Rename(link, launcher)
}

type cloudDirectory struct {
	path string
	info os.FileInfo
}

func cloudDirectories(root string) []cloudDirectory {
	entries, _ := os.ReadDir(root)
	dirs := []cloudDirectory{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if info, err := entry.Info(); err == nil {
			dirs = append(dirs, cloudDirectory{filepath.Join(root, entry.Name()), info})
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].info.ModTime().Equal(dirs[j].info.ModTime()) {
			return dirs[i].path > dirs[j].path
		}
		return dirs[i].info.ModTime().After(dirs[j].info.ModTime())
	})
	return dirs
}
func pruneCloudGenerations(root, binary string) {
	if err := markCloudManagedRoot(root); err != nil {
		return
	}
	gate, err := lockCloudFile(filepath.Join(root, "generations.lock"), false, false)
	if err != nil {
		return
	}
	defer gate.Close()
	active, _ := filepath.EvalSymlinks(filepath.Join(root, "bin", binary))
	current := cloudGeneration(active)
	// Never guess ownership when the current launcher cannot be resolved.
	if current == "" || cloudGenerationRoot(current) != root {
		return
	}
	previous, _ := filepath.EvalSymlinks(filepath.Join(current, "previous"))
	dirs := cloudDirectories(filepath.Join(root, "versions"))
	legacy := filepath.Join(root, "google-cloud-sdk")
	if info, err := os.Lstat(legacy); err == nil && info.IsDir() {
		dirs = append(dirs, cloudDirectory{legacy, info})
	}
	// Existing installations predate the previous pointer. Keep their most
	// recent spare generation as rollback until the next activation records it.
	_, declared := os.Stat(filepath.Join(current, ".activated"))
	if previous == "" && os.IsNotExist(declared) {
		for _, dir := range dirs {
			if dir.path != current {
				previous = dir.path
				break
			}
		}
	}
	for _, dir := range dirs {
		if dir.path == current || dir.path == previous {
			continue
		}
		lease, err := lockCloudFile(filepath.Join(dir.path, ".lease"), false, true)
		if err != nil {
			continue
		}
		_ = os.RemoveAll(dir.path)
		_ = lease.Close()
	}
}

// Backups are credential/config snapshots, not SDKs. Keep the two newest;
// shared leases additionally retain any backup involved in an active mutation.
func pruneCloudBackups(root string) {
	gate, err := lockCloudFile(filepath.Join(root, "backups.lock"), false, false)
	if err != nil {
		return
	}
	defer gate.Close()
	dirs := cloudDirectories(filepath.Join(root, "backups"))
	for i, dir := range dirs {
		if i < 2 {
			continue
		}
		lease, err := lockCloudFile(filepath.Join(dir.path, ".lease"), false, true)
		if err != nil {
			continue
		}
		_ = os.RemoveAll(dir.path)
		_ = lease.Close()
	}
}
func newCloudBackup(root, name string) (string, func(), error) {
	if err := os.MkdirAll(filepath.Join(root, "backups"), 0700); err != nil {
		return "", nil, err
	}
	gate, err := lockCloudFile(filepath.Join(root, "backups.lock"), false, false)
	if err != nil {
		return "", nil, err
	}
	defer gate.Close()
	path := filepath.Join(root, "backups", name)
	if err = os.Mkdir(path, 0700); err != nil {
		return "", nil, err
	}
	lease, err := lockCloudFile(filepath.Join(path, ".lease"), true, false)
	if err != nil {
		_ = os.RemoveAll(path)
		return "", nil, err
	}
	return path, func() { _ = lease.Close(); pruneCloudBackups(root) }, nil
}
