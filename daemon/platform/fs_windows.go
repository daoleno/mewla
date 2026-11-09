//go:build windows

package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// SyncDir does nothing on Windows: NTFS journals renames itself, and
// FlushFileBuffers on a directory handle fails with access denied, which
// would turn every atomic write into an error.
func SyncDir(string) error { return nil }

// SyncOpenDir does nothing on Windows, as SyncDir.
func SyncOpenDir(*os.File) error { return nil }

// MkdirPrivate creates dir, and any missing parents, and gives dir the ACL
// Codex requires of a control-socket directory: owned by this user, a
// protected DACL, and a single entry granting this user full access to it and
// everything created inside it. A directory that already exists gets that ACL
// too, since a 0700 mode means nothing on Windows and the inherited profile
// ACL also admits SYSTEM and Administrators.
func MkdirPrivate(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_, sid, err := CurrentUser()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("O:%sD:P(A;OICI;FA;;;%s)", sid, sid))
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil)
}
