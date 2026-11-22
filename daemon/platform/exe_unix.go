//go:build unix

package platform

import "os"

// RetireExecutable removes an obsolete backup, including one still executing.
func RetireExecutable(path, _ string) error { return os.Remove(path) }

// ReplaceFile moves source over target in one rename. A running executable
// can be replaced this way on Unix: the running image keeps its inode.
func ReplaceFile(source, target string) error { return os.Rename(source, target) }
