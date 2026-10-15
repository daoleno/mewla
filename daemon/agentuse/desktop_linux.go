package agentuse

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	uid := uint32(os.Getuid())
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	var fallback []string
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
		raw, err := os.ReadFile(filepath.Join(dir, "environ"))
		if err != nil {
			continue
		}
		vars := desktopFromEnviron(strings.Split(string(raw), "\x00"), socketExists)
		if len(vars) == 0 {
			continue
		}
		if exe, _ := os.Readlink(filepath.Join(dir, "exe")); strings.Contains(filepath.Base(exe), "chrome") {
			return vars, true
		}
		if fallback == nil {
			fallback = vars
		}
	}
	return fallback, fallback != nil
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
