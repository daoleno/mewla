package platform

import (
	"log"
	"runtime"
	"sync/atomic"
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
// ends. The watch runs on a thread of its own, started before the exec and
// waiting in the system (nanosleep): a stalled exec also keeps the runtime
// from starting a thread, so a Go timer could find no thread to run on (the
// macOS CI runner, 11-26: the exec after dispatch callbacks never happened).
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
	fns := libSystemCalls()
	var exited atomic.Bool
	watching := make(chan struct{})
	go func() {
		// Never unlocked: the thread ends with this goroutine, or with the
		// process image when the exec happens.
		runtime.LockOSThread()
		close(watching)
		fns.sleep(execStallAfter)
		if exited.Load() {
			return
		}
		log.Printf("mewla: exec of %s has not started after %s (the Go runtime waits for a preemption signal a system thread holds); calling execve directly", path, execStallAfter)
		if errno := fns.execve(pathp, argvp, envp); errno != 0 {
			log.Printf("mewla: execve %s: %v", path, errno)
		}
	}()
	<-watching
	err = syscall.Exec(path, argv, env)
	exited.Store(true)
	runtime.KeepAlive(pathp)
	runtime.KeepAlive(argvp)
	runtime.KeepAlive(envp)
	return err
}

// libSystem holds libc's execve and nanosleep, called outside the runtime's
// exec hooks and timers.
type libSystem struct{ execveFn, nanosleepFn uintptr }

func libSystemCalls() libSystem {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return libSystem{}
	}
	var calls libSystem
	calls.execveFn, _ = purego.Dlsym(lib, "execve")
	calls.nanosleepFn, _ = purego.Dlsym(lib, "nanosleep")
	return calls
}

// sleep waits d on this thread in the system; without nanosleep it returns
// at once (the direct execve then follows the exec sooner, not never).
func (l libSystem) sleep(d time.Duration) {
	if l.nanosleepFn == 0 {
		return
	}
	ts := syscall.NsecToTimespec(int64(d))
	purego.SyscallN(l.nanosleepFn, uintptr(unsafe.Pointer(&ts)), 0)
	runtime.KeepAlive(&ts)
}

// execve calls libc's execve; it returns only on failure.
func (l libSystem) execve(path *byte, argv, env []*byte) syscall.Errno {
	if l.execveFn == 0 {
		return syscall.ENOSYS
	}
	_, _, errno := purego.SyscallN(l.execveFn, uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&argv[0])), uintptr(unsafe.Pointer(&env[0])))
	if errno != 0 {
		return syscall.Errno(errno)
	}
	return syscall.EINVAL
}
