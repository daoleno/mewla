//go:build !linux

package gc

import (
	"io/fs"
	"time"
)

// accessTime is not read here: the t/ sweep runs on Linux only.
func accessTime(fs.FileInfo) time.Time { return time.Time{} }

// recordsReads is false here: access times are not read.
func recordsReads(string) bool { return false }
