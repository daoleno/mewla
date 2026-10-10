package modelprofiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// writeAtomicFile persists data to path through atomicfile, creating the
// parent directory.
func writeAtomicFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return persistError(atomicfile.Write(path, data, perm))
}

// writeAtomicFileWithSeams is writeAtomicFile with an owner's test failpoint
// hook and directory-sync seam.
func writeAtomicFileWithSeams(path string, data []byte, perm os.FileMode, hook func(string) error, dirSync func(string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return persistError(atomicfile.WriteOptions(path, data, perm, atomicfile.Options{Hook: hook, SyncDir: dirSync}))
}

// persistError reports a failure after the rename as ErrPersistDirSync: the
// write is applied, but its durability is unconfirmed.
func persistError(err error) error {
	if errors.Is(err, atomicfile.ErrDirSync) {
		return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
	}
	return err
}
