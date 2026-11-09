//go:build windows

package stats

import "os"

// fileIdentity is empty on Windows: size and modification time identify a
// usage file there.
func fileIdentity(info os.FileInfo) string { return "" }
