//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// ReplaceFile moves source over target. Windows cannot overwrite or delete an
// executable that is running, but it can rename it: so a target that exists
// is first renamed aside to <target>.old-<n>, then source takes its name. If
// that second rename fails the old file is put back. SweepReplaced removes the
// aside copies once nothing runs them.
func ReplaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	aside := target + replacedMarker + strconv.FormatInt(time.Now().UnixNano(), 10)
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
