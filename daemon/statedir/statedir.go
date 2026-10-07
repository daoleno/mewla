// Package statedir owns the default Mewla state root (~/.mewla) and the
// one-time migration from the legacy Zen root (~/.zen).
//
// Default is stateless so every package resolves the same root without
// coordination: a migrated (or fresh) ~/.mewla wins, a not-yet-migrated
// ~/.zen keeps serving until Migrate moves it. Migrate renames ~/.zen to
// ~/.mewla and leaves ~/.zen as a symlink to ~/.mewla, so absolute paths held
// by configs, tmux scripts, Brain notes and running processes keep resolving.
package statedir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DirName is the state root under the user's home directory.
	DirName = ".mewla"
	// LegacyDirName is the pre-rename Zen state root.
	LegacyDirName = ".zen"
	// MarkerName is written into the state root before it moves, so a
	// migrated root is distinguishable from an unrelated ~/.mewla.
	MarkerName = ".migrated-from-zen"

	stagingLinkName = ".zen.mewla-migrate"
)

// exchangeNames is swapped in tests to exercise the non-atomic fallback.
var exchangeNames = exchange

// Status classifies one Migrate outcome.
type Status string

const (
	// StatusFresh: neither root existed; nothing to migrate.
	StatusFresh Status = "fresh"
	// StatusCurrent: ~/.mewla is already the state root.
	StatusCurrent Status = "current"
	// StatusMigrated: this call moved ~/.zen to ~/.mewla.
	StatusMigrated Status = "migrated"
	// StatusDeferred: a daemon still holds the legacy root; keep using ~/.zen.
	StatusDeferred Status = "deferred"
	// StatusConflict: both roots exist independently; nothing was moved.
	StatusConflict Status = "conflict"
	// StatusUnsupported: ~/.zen cannot be renamed in place (for example a
	// mount point); keep using ~/.zen.
	StatusUnsupported Status = "unsupported"
)

// Result reports what Migrate did and which root is in effect.
type Result struct {
	Status  Status
	Root    string
	Message string
}

// Notable reports whether the result deserves a one-line operator notice.
func (r Result) Notable() bool {
	switch r.Status {
	case StatusMigrated, StatusConflict, StatusUnsupported, StatusDeferred:
		return true
	}
	return false
}

// DaemonLockPaths are the lifecycle locks a running daemon holds below the
// state root (main daemon and Brain daemon). They mirror control's lock name.
var DaemonLockPaths = []string{
	filepath.Join("run", "daemon.lock"),
	filepath.Join("brain", "run", "daemon.lock"),
}

// Default returns the state root for home without changing the filesystem.
func Default(home string) string {
	root := filepath.Join(home, DirName)
	legacy := filepath.Join(home, LegacyDirName)
	switch {
	case exists(root):
		if isRealDir(legacy) && !hasMarker(root) {
			// An unrelated ~/.mewla must never hide real Zen state.
			return legacy
		}
		return root
	case exists(legacy):
		return legacy
	default:
		return root
	}
}

// HomeDefault resolves Default for the current user's home directory.
func HomeDefault() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return Default(home), nil
}

// Migrate moves ~/.zen to ~/.mewla once. It is idempotent, never copies or
// deletes state, and refuses while a daemon holds the legacy root's lifecycle
// lock. The lock stays held across the rename so no daemon can start against
// a half-moved root.
func Migrate(home string) (Result, error) {
	root := filepath.Join(home, DirName)
	legacy := filepath.Join(home, LegacyDirName)

	legacyInfo, err := os.Lstat(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		if exists(root) && hasMarker(root) {
			// A crash after the move left no compatibility link; restore it.
			if err := linkLegacy(home); err != nil {
				return Result{Status: StatusCurrent, Root: root}, err
			}
		}
		if exists(root) {
			return Result{Status: StatusCurrent, Root: root}, nil
		}
		return Result{Status: StatusFresh, Root: root}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("inspect %s: %w", legacy, err)
	}
	if legacyInfo.Mode()&fs.ModeSymlink != 0 {
		if exists(root) {
			return Result{Status: StatusCurrent, Root: root}, nil
		}
		// A user-managed ~/.zen symlink: move the link itself so ~/.mewla
		// points at the same storage, then link ~/.zen back.
	} else if !legacyInfo.IsDir() {
		return Result{Status: StatusConflict, Root: Default(home),
			Message: fmt.Sprintf("%s is not a directory; leaving it in place", legacy)}, nil
	}
	if _, err := os.Lstat(root); err == nil {
		if hasMarker(root) {
			return Result{Status: StatusConflict, Root: root, Message: fmt.Sprintf(
				"%s reappeared after migration; Mewla uses %s. Merge or remove %s manually", legacy, root, legacy)}, nil
		}
		return Result{Status: StatusConflict, Root: legacy, Message: fmt.Sprintf(
			"both %s and %s exist; Mewla keeps using %s. Move one aside to migrate", legacy, root, legacy)}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect %s: %w", root, err)
	}

	locks, busy, err := lockDaemons(legacy)
	defer releaseLocks(locks)
	if err != nil {
		return Result{}, err
	}
	if busy != "" {
		return Result{Status: StatusDeferred, Root: legacy, Message: fmt.Sprintf(
			"a daemon still holds %s; stop it, then rerun Mewla to move %s to %s", busy, legacy, root)}, nil
	}

	marker := filepath.Join(legacy, MarkerName)
	stamp := fmt.Sprintf("migrated from %s at %s\n", legacy, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(marker, []byte(stamp), 0o600); err != nil {
		return Result{}, fmt.Errorf("mark %s for migration: %w", legacy, err)
	}
	if err := move(home); err != nil {
		_ = os.Remove(marker)
		if isUnsupportedMove(err) {
			return Result{Status: StatusUnsupported, Root: legacy, Message: fmt.Sprintf(
				"%s cannot be renamed in place (%v); Mewla keeps using it", legacy, err)}, nil
		}
		return Result{}, err
	}
	return Result{Status: StatusMigrated, Root: root, Message: fmt.Sprintf(
		"moved %s to %s; %s now links to it", legacy, root, legacy)}, nil
}

// move renames ~/.zen to ~/.mewla and leaves ~/.zen -> .mewla. Where the
// platform can atomically exchange two names, ~/.zen is never absent, so a
// concurrent `mkdir -p ~/.zen/...` cannot recreate a stray real directory.
func move(home string) error {
	root := filepath.Join(home, DirName)
	legacy := filepath.Join(home, LegacyDirName)
	staging := filepath.Join(home, stagingLinkName)

	if info, err := os.Lstat(staging); err == nil {
		if info.Mode()&fs.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a migration link; remove it and retry", staging)
		}
		if err := os.Remove(staging); err != nil {
			return fmt.Errorf("remove stale %s: %w", staging, err)
		}
	}
	if err := os.Symlink(DirName, staging); err != nil {
		return fmt.Errorf("create migration link: %w", err)
	}
	switch err := exchangeNames(staging, legacy); {
	case err == nil:
		// legacy is now the link and staging holds the real root.
		if err := os.Rename(staging, root); err != nil {
			_ = exchangeNames(staging, legacy)
			_ = os.Remove(staging)
			return fmt.Errorf("move %s to %s: %w", legacy, root, err)
		}
		return nil
	case errors.Is(err, errExchangeUnsupported):
		_ = os.Remove(staging)
	default:
		_ = os.Remove(staging)
		return fmt.Errorf("move %s to %s: %w", legacy, root, err)
	}

	if err := os.Rename(legacy, root); err != nil {
		return fmt.Errorf("move %s to %s: %w", legacy, root, err)
	}
	return linkLegacy(home)
}

// linkLegacy creates ~/.zen -> .mewla. A real directory recreated at ~/.zen
// in the meantime is reported, never replaced.
func linkLegacy(home string) error {
	legacy := filepath.Join(home, LegacyDirName)
	err := os.Symlink(DirName, legacy)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("link %s to %s: %w", legacy, DirName, err)
	}
	if target, readErr := os.Readlink(legacy); readErr == nil && isRootTarget(home, target) {
		return nil
	}
	return fmt.Errorf("%s was recreated during migration; Mewla uses %s. Merge %s into it manually",
		legacy, filepath.Join(home, DirName), legacy)
}

func isRootTarget(home, target string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(home, target)
	}
	return filepath.Clean(target) == filepath.Join(home, DirName)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isRealDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func hasMarker(root string) bool {
	info, err := os.Stat(filepath.Join(root, MarkerName))
	return err == nil && info.Mode().IsRegular()
}

// Notice formats a migration result as one operator-facing line.
func Notice(result Result) string {
	message := strings.TrimSpace(result.Message)
	if message == "" {
		return ""
	}
	return "mewla: state: " + message
}
