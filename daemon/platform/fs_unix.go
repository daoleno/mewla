//go:build unix

package platform

import "os"

// SyncDir fsyncs a directory so a rename or create inside it is durable.
func SyncDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// SyncOpenDir fsyncs an already open directory.
func SyncOpenDir(dir *os.File) error { return dir.Sync() }
