//go:build windows

package platform

import "os"

// SyncDir does nothing on Windows: NTFS journals renames itself, and
// FlushFileBuffers on a directory handle fails with access denied, which
// would turn every atomic write into an error.
func SyncDir(string) error { return nil }

// SyncOpenDir does nothing on Windows, as SyncDir.
func SyncOpenDir(*os.File) error { return nil }
