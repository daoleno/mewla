// Package logfile keeps a size-rotated copy of the daemon's log lines under
// the state directory, so problems stay inspectable after the terminal that
// showed them is gone.
package logfile

import (
	"os"
	"path/filepath"
	"sync"
)

// Default size limits: the live file and one rotated file, 10 MB each.
const DefaultMaxBytes = 10 << 20

// Path returns the daemon log file for a state directory.
func Path(stateDir string) string {
	return filepath.Join(stateDir, "logs", "daemon.log")
}

// RotatedPath returns the file the live log rotates into.
func RotatedPath(path string) string {
	return path + ".1"
}

// Writer appends to a file and rotates it to <path>.1 once it would exceed
// MaxBytes. It is safe for concurrent use.
type Writer struct {
	path     string
	maxBytes int64

	mu   sync.Mutex
	file *os.File
	size int64
}

// Open opens or creates the log file at path.
func Open(path string, maxBytes int64) (*Writer, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w := &Writer{path: path, maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}

// Write appends p, rotating first when p would push the file past the limit.
// A failure to write the file never fails the caller's log line.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if w.open() != nil {
			return len(p), nil
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		_ = w.file.Close()
		w.file = nil
		_ = os.Rename(w.path, RotatedPath(w.path))
		if w.open() != nil {
			return len(p), nil
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		return len(p), nil
	}
	return n, nil
}

// Close closes the current file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
