package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// replacedMarker names the copies a Windows swap moves aside:
// <exe>.old-<unix nanos>.
const replacedMarker = ".old-"

// SweepReplaced removes the copies ReplaceFile moved aside for target. A copy
// still running (a session host started from it) cannot be removed on
// Windows and is left for a later sweep.
func SweepReplaced(target string) {
	matches, _ := filepath.Glob(target + replacedMarker + "*")
	for _, path := range matches {
		if strings.HasPrefix(filepath.Base(path), filepath.Base(target)+replacedMarker) {
			_ = os.Remove(path)
		}
	}
}

// IsReplacedCopy reports whether path is a copy ReplaceFile moved aside for
// target: a running executable an update replaced.
func IsReplacedCopy(path, target string) bool {
	return filepath.Dir(path) == filepath.Dir(target) &&
		strings.HasPrefix(filepath.Base(path), filepath.Base(target)+replacedMarker)
}
