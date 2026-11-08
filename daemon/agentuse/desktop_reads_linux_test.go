//go:build linux

package agentuse

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func selfReadSyscalls(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("no /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(line, "syscr: "); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	t.Fatal("no syscr")
	return 0
}

// The status of the browser is refreshed every 30 seconds while it is not
// ready. Finding the desktop to borrow from Chrome read the environment of
// every process of the person until it met one: a thousand reads each time.
func TestFindDesktopReadsOnlyChromesEnvironment(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"DISPLAY", "WAYLAND_DISPLAY"} {
		t.Setenv(name, "")
	}
	socket := filepath.Join(dir, "wayland-test")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("no unix socket: %v", err)
	}
	defer listener.Close()

	source, err := os.Open("/bin/sleep")
	if err != nil {
		t.Skipf("no sleep: %v", err)
	}
	chrome := filepath.Join(dir, "chrome")
	target, err := os.OpenFile(chrome, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	source.Close()
	target.Close()
	cmd := exec.Command(chrome, "60")
	cmd.Env = []string{"WAYLAND_DISPLAY=" + socket, "XDG_RUNTIME_DIR=" + dir}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	time.Sleep(50 * time.Millisecond)

	desktopDonor.Lock()
	desktopDonor.pid, desktopDonor.start, desktopDonor.emptyAt = "", "", time.Time{}
	desktopDonor.Unlock()

	before := selfReadSyscalls(t)
	vars, ok := findDesktop()
	walk := selfReadSyscalls(t) - before
	if !ok || !strings.Contains(strings.Join(vars, " "), "WAYLAND_DISPLAY="+socket) {
		t.Fatalf("desktop = %v, %v", vars, ok)
	}
	// A handful of reads per Chrome process (any Chrome of this machine may be
	// met first), and none for the rest.
	if limit := uint64(16*countChromeProcesses()) + 20; walk > limit {
		t.Fatalf("finding the desktop took %d read syscalls, more than %d for the Chrome processes", walk, limit)
	}

	// The Chrome that was borrowed from is asked again before any walk.
	before = selfReadSyscalls(t)
	if vars, ok := findDesktop(); !ok || len(vars) == 0 {
		t.Fatalf("second lookup = %v, %v", vars, ok)
	}
	if again := selfReadSyscalls(t) - before; again > 8 {
		t.Fatalf("the second lookup took %d read syscalls", again)
	}
}

func countChromeProcesses() int {
	entries, _ := os.ReadDir("/proc")
	count := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		if exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe")); err == nil && strings.Contains(filepath.Base(exe), "chrome") {
			count++
		}
	}
	return count
}
