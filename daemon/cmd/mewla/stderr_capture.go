package main

import (
	"os"
	"path/filepath"
)

// A daemon launchd runs for Mewla for Mac (through the app's host) has its
// output thrown away: the job names no file for it. What only reaches stderr
// was then lost: a panic, the runtime's goroutine dump on SIGQUIT, and the
// last lines of a restart, written after the daemon log is closed. The
// owner's Mac, 11-26: a restart stalled for 12 minutes and nothing said
// where. So a daemon whose stderr is /dev/null puts it in
// logs/daemon-stderr.log instead; an exec in place keeps it.

const (
	stderrCaptureName = "daemon-stderr.log"
	// stderrCaptureMax: a larger file moves to .1 when a daemon starts.
	stderrCaptureMax = 4 << 20
)

// stderrCaptured: this daemon's stderr is the capture file, which then holds
// only what bypasses the log (the log goes to the daemon log alone).
var stderrCaptured bool

// captureDiscardedStderr points fd 2 at the capture file under stateDir when
// it is /dev/null, and says whether it did.
func captureDiscardedStderr(stateDir string) bool {
	if !discarded(os.Stderr) {
		return false
	}
	path := filepath.Join(stateDir, "logs", stderrCaptureName)
	file, err := openStderrCapture(path)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := redirectFD(file, 2); err != nil {
		return false
	}
	stderrCaptured = true
	return true
}

// discarded: file is the null device.
func discarded(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err == nil && os.SameFile(info, null)
}

// openStderrCapture opens path for appending, private, after moving a file
// past stderrCaptureMax to path.1.
func openStderrCapture(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > stderrCaptureMax {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
