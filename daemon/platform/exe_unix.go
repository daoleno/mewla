//go:build unix

package platform

import "os"

// ReplaceFile moves source over target in one rename. A running executable
// can be replaced this way on Unix: the running image keeps its inode.
func ReplaceFile(source, target string) error { return os.Rename(source, target) }
