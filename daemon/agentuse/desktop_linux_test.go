package agentuse

import (
	"reflect"
	"testing"
)

func TestDesktopVariablesNeedADisplayThatExists(t *testing.T) {
	live := map[string]bool{"/run/user/1000/wayland-0": true, "/tmp/.X11-unix/X0": true}
	exists := func(path string) bool { return live[path] }
	if got := desktopFromEnviron([]string{"XDG_RUNTIME_DIR=/run/user/1000", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus", "HOME=/h"}, exists); got != nil {
		t.Fatalf("a session bus is not a display: %v", got)
	}
	got := desktopFromEnviron([]string{"HOME=/h", "WAYLAND_DISPLAY=wayland-0", "DISPLAY=", "XDG_RUNTIME_DIR=/run/user/1000"}, exists)
	if want := []string{"WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=/run/user/1000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// A session that ended, and a container's display: neither is a desktop.
	for _, environ := range [][]string{
		{"WAYLAND_DISPLAY=wayland-1", "DISPLAY=:1", "XDG_RUNTIME_DIR=/run/user/1000"},
		{"DISPLAY=:99"},
		{"DISPLAY=remote:0"},
	} {
		if got := desktopFromEnviron(environ, exists); got != nil {
			t.Fatalf("%v counted as a desktop: %v", environ, got)
		}
	}
	// A live X display survives next to a stale Wayland one.
	got = desktopFromEnviron([]string{"WAYLAND_DISPLAY=wayland-1", "DISPLAY=:0.0", "XAUTHORITY=/h/.Xauthority", "XDG_RUNTIME_DIR=/run/user/1000"}, exists)
	if want := []string{"DISPLAY=:0.0", "XAUTHORITY=/h/.Xauthority", "XDG_RUNTIME_DIR=/run/user/1000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
