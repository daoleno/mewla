package agentuse

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/workerproc"
)

// desktopVars are what Chrome on Linux needs to open on the user's desktop.
var desktopVars = []string{"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"}

// findDesktop returns the user's desktop session for Chrome. A daemon started
// at boot has none in its own environment, so it borrows the session from the
// user's running Chrome, or else from any of their desktop programs.
func findDesktop() ([]string, bool) {
	if vars := desktopFromEnviron(os.Environ(), socketExists); len(vars) > 0 {
		return vars, true
	}
	if vars, ok := desktopFromDonor(); ok {
		return vars, true
	}
	uid := uint32(os.Getuid())
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	// Chrome is told from the other programs by its executable, which costs no
	// read; only its environment is read. Reading the environment of every
	// process of the user to find it was a thousand reads each time the status
	// was refreshed.
	var others []string
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uid {
			continue
		}
		exe, _ := os.Readlink(filepath.Join(dir, "exe"))
		if !strings.Contains(filepath.Base(exe), "chrome") {
			others = append(others, entry.Name())
			continue
		}
		if vars := desktopOfProcess(entry.Name()); len(vars) > 0 {
			rememberDesktopDonor(entry.Name())
			return vars, true
		}
	}
	// No Chrome shows a desktop: any other program of the person's that does.
	// This walk is the expensive one, so a walk that found nothing is not
	// repeated for desktopWalkPause.
	desktopDonor.Lock()
	pause := time.Since(desktopDonor.emptyAt) < desktopWalkPause
	desktopDonor.Unlock()
	if pause {
		return nil, false
	}
	for _, pid := range others {
		if vars := desktopOfProcess(pid); len(vars) > 0 {
			return vars, true
		}
	}
	desktopDonor.Lock()
	desktopDonor.emptyAt = time.Now()
	desktopDonor.Unlock()
	return nil, false
}

// desktopWalkPause is how long a walk over the person's other programs, which
// found no desktop, is not repeated.
const desktopWalkPause = 5 * time.Minute

// desktopOfProcess is the desktop one process's environment names.
func desktopOfProcess(pid string) []string {
	raw, err := os.ReadFile(filepath.Join("/proc", pid, "environ"))
	if err != nil {
		return nil
	}
	return desktopFromEnviron(strings.Split(string(raw), "\x00"), socketExists)
}

// desktopFromEnviron keeps the desktop variables of one environment, or
// nothing when it names no display that exists now. Programs keep the
// variables of a session that has since ended, and a container's display is
// not this computer's, so each display is checked for its socket.
func desktopFromEnviron(environ []string, exists func(path string) bool) []string {
	values := map[string]string{}
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if ok && value != "" {
			values[name] = value
		}
	}
	if wayland := values["WAYLAND_DISPLAY"]; wayland != "" {
		path := wayland
		if !filepath.IsAbs(path) {
			path = filepath.Join(values["XDG_RUNTIME_DIR"], wayland)
		}
		if values["XDG_RUNTIME_DIR"] == "" && !filepath.IsAbs(wayland) || !exists(path) {
			delete(values, "WAYLAND_DISPLAY")
		}
	}
	if display := values["DISPLAY"]; display != "" {
		if !exists(x11Socket(display)) {
			delete(values, "DISPLAY")
			delete(values, "XAUTHORITY")
		}
	}
	if values["DISPLAY"] == "" && values["WAYLAND_DISPLAY"] == "" {
		return nil
	}
	var vars []string
	for _, name := range desktopVars {
		if value := values[name]; value != "" {
			vars = append(vars, name+"="+value)
		}
	}
	return vars
}

// x11Socket is the local socket of a display like ":1" or ":1.0"; a display
// on another host has none.
func x11Socket(display string) string {
	if !strings.HasPrefix(display, ":") {
		return ""
	}
	number, _, _ := strings.Cut(display[1:], ".")
	if _, err := strconv.Atoi(number); err != nil {
		return ""
	}
	return "/tmp/.X11-unix/X" + number
}

func socketExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

// desktopDonor is the Chrome process the last walk of /proc took the desktop
// from. Looking again at that one process, and walking only when it is gone or
// no longer names a desktop, turns a read of every process's environment each
// time the status is refreshed into two reads.
var desktopDonor struct {
	sync.Mutex
	pid   string
	start string
	// emptyAt is when a walk over the other programs last found no desktop.
	emptyAt time.Time
}

func rememberDesktopDonor(pid string) {
	number, err := strconv.Atoi(pid)
	if err != nil {
		return
	}
	process, err := workerproc.ReadProcStat(number)
	if err != nil {
		return
	}
	desktopDonor.Lock()
	desktopDonor.pid, desktopDonor.start = pid, process.Start
	desktopDonor.Unlock()
}

func desktopFromDonor() ([]string, bool) {
	desktopDonor.Lock()
	pid, start := desktopDonor.pid, desktopDonor.start
	desktopDonor.Unlock()
	number, err := strconv.Atoi(pid)
	if err != nil {
		return nil, false
	}
	// The start time fences a reused pid.
	if process, err := workerproc.ReadProcStat(number); err != nil || process.Start != start {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join("/proc", pid, "environ"))
	if err != nil {
		return nil, false
	}
	vars := desktopFromEnviron(strings.Split(string(raw), "\x00"), socketExists)
	return vars, len(vars) > 0
}
