package main

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var pidPath struct {
	once sync.Once
	fn   func(pid int32, buf unsafe.Pointer, size uint32) int32
}

// processPath is the executable a process runs, where it is now (proc_pidpath
// follows the file: an app moved aside reads at its new place).
func processPath(pid int) (string, error) {
	pidPath.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		if _, err := purego.Dlsym(lib, "proc_pidpath"); err != nil {
			return
		}
		purego.RegisterLibFunc(&pidPath.fn, lib, "proc_pidpath")
	})
	if pidPath.fn == nil {
		return "", errors.New("proc_pidpath is not available")
	}
	buf := make([]byte, 4096) // PROC_PIDPATHINFO_MAXSIZE
	n := pidPath.fn(int32(pid), unsafe.Pointer(&buf[0]), uint32(len(buf)))
	if n <= 0 || int(n) > len(buf) {
		return "", errors.New("proc_pidpath failed")
	}
	return string(buf[:n]), nil
}
