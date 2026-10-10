// Package atomicfile replaces files so a crash leaves either the old or the
// new content on disk, never a torn write.
//
// Every write goes to a hidden temporary file beside the target
// (".<name>.tmp-*"), is chmodded to the caller's mode independent of the
// umask, fsynced, renamed over the target and followed by an fsync of the
// parent directory. The parent directory must already exist: callers own its
// mode.
package atomicfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrDirSync marks a failure after the rename: the new content has replaced
// the file, but the directory entry may not survive a crash. Callers that
// report durability separately test for it with errors.Is; callers that treat
// the directory sync as best-effort ignore it.
var ErrDirSync = errors.New("file replaced but parent directory sync failed")

// Options are fault seams for callers whose tests exercise each persistence
// phase. Production callers use Write or WriteStream.
type Options struct {
	// Hook, when set, runs at before_write, after_write, before_sync,
	// after_sync, before_rename, after_rename, before_dirsync and
	// after_dirsync. An error before the rename aborts the write; an error
	// after it is wrapped in ErrDirSync.
	Hook func(phase string) error
	// SyncDir replaces the parent directory fsync.
	SyncDir func(dir string) error
}

// Write atomically replaces path with data at mode perm.
func Write(path string, data []byte, perm os.FileMode) error {
	return WriteOptions(path, data, perm, Options{})
}

// WriteOptions is Write with fault seams.
func WriteOptions(path string, data []byte, perm os.FileMode, options Options) error {
	return write(path, perm, options, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

// WriteStream atomically replaces path with whatever fill writes, buffered,
// for content too large to build in memory first.
func WriteStream(path string, perm os.FileMode, fill func(io.Writer) error) error {
	return write(path, perm, Options{}, func(file *os.File) error {
		buffered := bufio.NewWriter(file)
		if err := fill(buffered); err != nil {
			return err
		}
		return buffered.Flush()
	})
}

func write(path string, perm os.FileMode, options Options, fill func(*os.File) error) error {
	phase := func(name string) error {
		if options.Hook == nil {
			return nil
		}
		return options.Hook(name)
	}
	dir := filepath.Dir(path)
	if err := phase("before_write"); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := fill(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	for _, name := range []string{"after_write", "before_sync"} {
		if err := phase(name); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := phase("after_sync"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := phase("before_rename"); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	renamed = true
	if err := phase("after_rename"); err != nil {
		return fmt.Errorf("%w: %w", ErrDirSync, err)
	}
	if err := phase("before_dirsync"); err != nil {
		return fmt.Errorf("%w: %w", ErrDirSync, err)
	}
	syncDir := options.SyncDir
	if syncDir == nil {
		syncDir = SyncDir
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%w: %w", ErrDirSync, err)
	}
	if err := phase("after_dirsync"); err != nil {
		return fmt.Errorf("%w: %w", ErrDirSync, err)
	}
	return nil
}

// SyncDir fsyncs a directory so a rename or create inside it is durable.
func SyncDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
