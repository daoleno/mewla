//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	detachedFlags = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
	breakaway     = windows.CREATE_BREAKAWAY_FROM_JOB
)

// StartDetached starts cmd with no console and outside the caller's job, so
// it outlives the caller, its console window and an SSH or Task Scheduler
// session. A job that forbids breakaway refuses the flag; the start is then
// retried inside the job.
func StartDetached(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	base := cmd.SysProcAttr.CreationFlags
	cmd.SysProcAttr.CreationFlags = base | detachedFlags | breakaway
	err := cmd.Start()
	if err != nil && errors.Is(err, windows.ERROR_ACCESS_DENIED) && cmd.Process == nil {
		retry := exec.Command(cmd.Path, cmd.Args[1:]...)
		retry.Env, retry.Dir = cmd.Env, cmd.Dir
		retry.Stdin, retry.Stdout, retry.Stderr = cmd.Stdin, cmd.Stdout, cmd.Stderr
		retry.ExtraFiles = cmd.ExtraFiles
		retry.SysProcAttr = &syscall.SysProcAttr{CreationFlags: base | detachedFlags}
		if retryErr := retry.Start(); retryErr != nil {
			return retryErr
		}
		*cmd = *retry
		return nil
	}
	return err
}

// ExecReplace stands in for exec on Windows, which cannot replace a running
// image: it starts path detached with the same arguments and environment,
// then ends this process. The new process gets a new PID.
func ExecReplace(path string, argv, env []string) error {
	args := []string(nil)
	if len(argv) > 1 {
		args = argv[1:]
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := StartDetached(cmd); err != nil {
		return fmt.Errorf("start %s: %w", path, err)
	}
	_ = cmd.Process.Release()
	os.Exit(0)
	return nil
}

// ProcessAlive reports whether pid names a running process.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied means it exists but belongs to someone else.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == 259 // STILL_ACTIVE
}

// ProcessStartTime is when pid was created; with the pid it identifies one
// process, since Windows reuses pids.
func ProcessStartTime(pid int) (time.Time, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, err
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, creation.Nanoseconds()), nil
}
