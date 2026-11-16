//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

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
func StartDetached(cmd *exec.Cmd) error { return startOutsideJob(cmd, detachedFlags) }

// StartDetachedBare starts the program at path as StartDetached does, but
// gives it no standard handles, as the Task Scheduler starts a task's
// program. A headless console host (conhost --headless) takes its standard
// input and output for its pseudo console: handed any (Go hands a child the
// null device for each one it lacks), it reads the end of that input at once
// and ends, and the program it hosts ends with it.
func StartDetachedBare(path string, args []string, dir string) error {
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{path}, args...)))
	if err != nil {
		return err
	}
	var cwd *uint16
	if dir != "" {
		if cwd, err = windows.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}
	start := func(flags uint32) error {
		info := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
		var process windows.ProcessInformation
		if err := windows.CreateProcess(app, line, nil, nil, false, flags, nil, cwd, &info, &process); err != nil {
			return err
		}
		_ = windows.CloseHandle(process.Thread)
		return windows.CloseHandle(process.Process)
	}
	err = start(detachedFlags | breakaway)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		err = start(detachedFlags)
	}
	if err != nil {
		return fmt.Errorf("start %s: %w", path, err)
	}
	return nil
}

// StartBackground starts cmd outside the caller's job, as StartDetached, but
// with a console of its own that has no window. Console programs it starts
// share that console; started from a process with no console at all, each
// would open a console window on the person's desktop.
func StartBackground(cmd *exec.Cmd) error {
	return startOutsideJob(cmd, windows.CREATE_NO_WINDOW|windows.CREATE_NEW_PROCESS_GROUP)
}

func startOutsideJob(cmd *exec.Cmd, detachedFlags uint32) error {
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

// ExecInPlace is the closest Windows has to execve. path starts as a child
// in this console with this process's standard handles, argv, env and dir, so
// its output goes to the same window and Ctrl+C reaches it. This process stays
// and waits: the shell that started it keeps waiting too (its prompt does not
// come back under the child's output, and a prompt reading the console would
// swallow Ctrl+C), and it exits with the child's exit code. Ctrl+C reaches
// both; stops, which the caller registered for os.Interrupt, keeps it from
// ending this process, which ends when the child does. It returns only when
// the child could not be started or waited for.
func ExecInPlace(path string, argv, env []string, dir string, stops <-chan os.Signal) error {
	cmd := exec.Command(path)
	if len(argv) > 0 {
		cmd.Args = argv
	}
	cmd.Env, cmd.Dir = env, dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if stops != nil {
		go func() {
			for range stops {
			}
		}()
	}
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	if err != nil {
		return err
	}
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

// ProcessImageName is the executable file of pid, as a full path.
func ProcessImageName(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// ProcessImagePath is ProcessImageName. A running executable moved aside by
// an update still reads as the path it started from.
func ProcessImagePath(pid int) (string, error) { return ProcessImageName(pid) }

// ParentImage is the executable file name of pid's parent ("conhost.exe"),
// or ErrParentGone when that parent has ended.
func ParentImage(pid int) (string, error) {
	list, err := ProcessList()
	if err != nil {
		return "", err
	}
	parent := 0
	for _, entry := range list {
		if entry.PID == pid {
			parent = entry.PPID
		}
	}
	for _, entry := range list {
		if parent != 0 && entry.PID == parent {
			return entry.Image, nil
		}
	}
	if parent == 0 {
		return "", fmt.Errorf("no process %d", pid)
	}
	return "", fmt.Errorf("parent %d of %d: %w", parent, pid, ErrParentGone)
}

// ProcessEntry is one process in a ProcessList.
type ProcessEntry struct {
	PID, PPID int
	// Image is the executable's file name ("claude.exe").
	Image string
}

// ProcessList is every process of the machine at one moment.
func ProcessList() ([]ProcessEntry, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var list []ProcessEntry
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		list = append(list, ProcessEntry{PID: int(entry.ProcessID), PPID: int(entry.ParentProcessID), Image: windows.UTF16ToString(entry.ExeFile[:])})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return list, nil
}

// ProcessCommandLine is pid's command line, for a process this user may
// query.
func ProcessCommandLine(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buf := make([]byte, 4096)
	for {
		var size uint32
		err := windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), uint32(len(buf)), &size)
		if err == nil {
			return (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0])).String(), nil
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) || int(size) <= len(buf) || size > 1<<20 {
			return "", err
		}
		buf = make([]byte, size)
	}
}

// ProcessCreationFiletime is when pid started, as the FILETIME (100 ns ticks
// since 1601) Windows records. A pid with no process is os.ErrNotExist.
func ProcessCreationFiletime(pid int) (uint64, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return 0, fmt.Errorf("process %d: %w", pid, os.ErrNotExist)
	}
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err == nil && code != 259 { // STILL_ACTIVE
		return 0, fmt.Errorf("process %d exited: %w", pid, os.ErrNotExist)
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}
