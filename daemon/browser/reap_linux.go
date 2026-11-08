//go:build linux

package browser

import (
	"bytes"
	"log"
	"os"
	"strconv"
	"syscall"
)

// reapStrays ends what a daemon that died without closing its browser left
// running: every process a runtime starts carries the pid of the daemon that
// started it (ownerEnv), and Pdeathsig only reaches a direct child, so KWin
// under its bus and Chrome's helpers outlive a crash and keep holding the profile. Only this user's processes
// whose owner is gone are signalled.
func reapStrays() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	prefix := []byte(ownerEnv + "=")
	for _, f := range entries {
		pid, err := strconv.Atoi(f.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile("/proc/" + f.Name() + "/environ")
		if err != nil {
			continue
		}
		for _, kv := range bytes.Split(env, []byte{0}) {
			if !bytes.HasPrefix(kv, prefix) {
				continue
			}
			owner, err := strconv.Atoi(string(kv[len(prefix):]))
			if err != nil || owner == os.Getpid() {
				break
			}
			if err := syscall.Kill(owner, 0); err == syscall.ESRCH {
				if syscall.Kill(pid, syscall.SIGTERM) == nil {
					log.Printf("browser: ended process %d left by daemon %d", pid, owner)
				}
			}
			break
		}
	}
}
