//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

// RetireExecutable removes an obsolete backup. A previous updater may have
// hard-linked it to an image that still hosts Sessions; Windows allows the
// link to move aside until SweepReplaced can delete it.
func RetireExecutable(path, installed string) error {
	err := os.Remove(path)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return err
	}
	return os.Rename(path, replacedPath(installed))
}

func replacedPath(target string) string {
	return target + replacedMarker + strconv.FormatInt(time.Now().UnixNano(), 10)
}

// ReplaceFile moves source over target. Windows cannot overwrite or delete an
// executable that is running, but it can rename it: so a target that exists
// is first renamed aside to <target>.old-<n>, then source takes its name. If
// that second rename fails the old file is put back. SweepReplaced removes the
// aside copies once nothing runs them.
func ReplaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	aside := replacedPath(target)
	if err := os.Rename(target, aside); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.Rename(source, target)
		}
		return fmt.Errorf("move the running executable aside: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		if restoreErr := os.Rename(aside, target); restoreErr != nil {
			return fmt.Errorf("install %s: %w; and %s could not be put back: %v", target, err, aside, restoreErr)
		}
		return err
	}
	return nil
}
