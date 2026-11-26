package platform

import (
	"log"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// On macOS, Go's syscall.Exec first waits until every preemption signal the
// runtime sent has been received (runtime_BeforeExec, Go issue 41702). A
// signal sent to a thread of the system's own (a dispatch queue's) while it
// ran a Go callback can stay pending there for good: the thread blocks it
// again once the callback returns. The daemon's macOS code gets callbacks on
// such threads (ScreenCaptureKit, Accessibility), so its exec could wait
// forever, still holding the runtime's exec lock: the owner's MacBook,
// 11-26, 0.4.31 → 0.4.32, 12 minutes until SIGQUIT; the same Mac stalls 3 in 3
// times with dispatch-queue callbacks before an exec and never without them.
//
// So the exec gets execStallAfter; past it, execve is called directly. That
// is safe then: the stalled exec holds the exec lock, so the runtime sends no
// new preemption signal, and the one left pending sits on a thread execve
// ends.
const execStallAfter = 3 * time.Second

func execve(path string, argv, env []string) error {
	pathp, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	argvp, err := syscall.SlicePtrFromStrings(argv)
	if err != nil {
		return err
	}
	envp, err := syscall.SlicePtrFromStrings(env)
	if err != nil {
		return err
	}
	// Resolved now: the stalled runtime should be asked for as little as can be.
	fn := libcExecve()
	timer := time.AfterFunc(execStallAfter, func() {
		log.Printf("mewla: exec of %s has not started after %s (the Go runtime waits for a preemption signal a system thread holds); calling execve directly", path, execStallAfter)
		if errno := directExecve(fn, pathp, argvp, envp); errno != 0 {
			log.Printf("mewla: execve %s: %v", path, errno)
		}
	})
	err = syscall.Exec(path, argv, env)
	timer.Stop()
	runtime.KeepAlive(pathp)
	runtime.KeepAlive(argvp)
	runtime.KeepAlive(envp)
	return err
}

// libcExecve is execve in libSystem, 0 if it cannot be found.
func libcExecve() uintptr {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0
	}
	fn, err := purego.Dlsym(lib, "execve")
	if err != nil {
		return 0
	}
	return fn
}

// directExecve calls execve (fn) outside the runtime's exec hooks. It returns
// only on failure.
func directExecve(fn uintptr, path *byte, argv, env []*byte) syscall.Errno {
	if fn == 0 {
		return syscall.ENOSYS
	}
	_, _, errno := purego.SyscallN(fn, uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&argv[0])), uintptr(unsafe.Pointer(&env[0])))
	if errno != 0 {
		return syscall.Errno(errno)
	}
	return syscall.EINVAL
}
