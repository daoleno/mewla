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
