//go:build linux

package userunit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	systemdDest = "org.freedesktop.systemd1"
	systemdPath = dbus.ObjectPath("/org/freedesktop/systemd1")
	manager     = "org.freedesktop.systemd1.Manager"

	// scopeStopTimeout: what a stopped scope's processes get between SIGTERM
	// and SIGKILL (systemd's default is 90 s).
	scopeStopTimeout = 10 * time.Second
	// placeWait bounds the wait for a started scope to hold its process.
	placeWait = 2 * time.Second
)

// EnclosingSlice is the innermost Session slice this process runs in, ""
// when none (see EnclosingSliceOf).
func EnclosingSlice() string {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	return EnclosingSliceOf(string(raw))
}

// Available reports whether there is a user bus to reach the user manager on.
func Available() bool {
	_, err := os.Stat(filepath.Join(fmt.Sprintf("/run/user/%d", os.Getuid()), "bus"))
	return err == nil
}

// connect reaches the person's user manager over their session bus, whatever
// this process's environment says.
func connect() (*dbus.Conn, error) {
	bus := filepath.Join(fmt.Sprintf("/run/user/%d", os.Getuid()), "bus")
	if _, err := os.Stat(bus); err != nil {
		return nil, ErrUnsupported
	}
	conn, err := dbus.Connect("unix:path=" + bus)
	if err != nil {
		return nil, fmt.Errorf("user bus: %w", err)
	}
	return conn, nil
}

// PlaceInScope starts a scope of its own for pid in slice: a new scope per
// program, so that a relaunch in the same Session never meets the scope of
// the program before it. OOMPolicy=continue: the kernel's OOM victim dies,
// not the whole Session (systemd's default stops the unit). It returns once
// pid is in the scope.
func PlaceInScope(pid int, slice, description string) (string, error) {
	if pid <= 1 || !isSessionSlice(slice) {
		return "", fmt.Errorf("place pid %d in %q: not a Session slice", pid, slice)
	}
	conn, err := connect()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	scope := strings.TrimSuffix(slice, sliceSuffix) + "-p" + hex.EncodeToString(suffix[:]) + scopeSuffix
	props := []struct {
		Name  string
		Value dbus.Variant
	}{
		{"Description", dbus.MakeVariant(description)},
		{"PIDs", dbus.MakeVariant([]uint32{uint32(pid)})},
		{"Slice", dbus.MakeVariant(slice)},
		{"OOMPolicy", dbus.MakeVariant("continue")},
		{"TimeoutStopUSec", dbus.MakeVariant(uint64(scopeStopTimeout / time.Microsecond))},
		{"CollectMode", dbus.MakeVariant("inactive-or-failed")},
	}
	aux := []struct {
		Name  string
		Props []struct {
			Name  string
			Value dbus.Variant
		}
	}{}
	var job dbus.ObjectPath
	if err := conn.Object(systemdDest, systemdPath).Call(manager+".StartTransientUnit", 0, scope, "fail", props, aux).Store(&job); err != nil {
		return "", fmt.Errorf("start %s: %w", scope, err)
	}
	cgroupFile := fmt.Sprintf("/proc/%d/cgroup", pid)
	for deadline := time.Now().Add(placeWait); ; {
		raw, err := os.ReadFile(cgroupFile)
		if err != nil {
			return "", fmt.Errorf("place pid %d in %s: %w", pid, scope, err)
		}
		if strings.Contains(string(raw), "/"+scope+"\n") || strings.HasSuffix(strings.TrimSpace(string(raw)), "/"+scope) {
			return scope, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("pid %d is not in %s after %s", pid, scope, placeWait)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// StopSession stops the slice of the Session whose resource unit is unit,
// with everything in it, and waits up to wait for it to have stopped. It
// returns the slices it stopped; none (and no error) when the Session has
// no slice, ErrUnsupported when there is no user manager to ask.
func StopSession(unit string, wait time.Duration) ([]string, error) {
	token := Token(unit)
	if token == "" {
		return nil, nil
	}
	conn, err := connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	obj := conn.Object(systemdDest, systemdPath)
	active := func() ([]string, error) {
		var units []struct {
			Name, Description, LoadState, ActiveState, SubState, Following string
			Path                                                           dbus.ObjectPath
			JobID                                                          uint32
			JobType                                                        string
			JobPath                                                        dbus.ObjectPath
		}
		if err := obj.Call(manager+".ListUnitsByPatterns", 0, []string{}, []string{sessionSlicePattern(token)}).Store(&units); err != nil {
			return nil, fmt.Errorf("list the slices of %s: %w", unit, err)
		}
		var names []string
		for _, u := range units {
			if u.ActiveState == "active" || u.ActiveState == "activating" || u.ActiveState == "deactivating" || u.ActiveState == "reloading" {
				names = append(names, u.Name)
			}
		}
		return names, nil
	}
	slices, err := active()
	if err != nil || len(slices) == 0 {
		return nil, err
	}
	for _, slice := range slices {
		var job dbus.ObjectPath
		if err := obj.Call(manager+".StopUnit", 0, slice, "replace").Store(&job); err != nil {
			return nil, fmt.Errorf("stop %s: %w", slice, err)
		}
	}
	for deadline := time.Now().Add(wait); ; {
		left, err := active()
		if err != nil {
			return slices, err
		}
		if len(left) == 0 {
			return slices, nil
		}
		if time.Now().After(deadline) {
			return slices, fmt.Errorf("%s still stopping after %s", strings.Join(left, ", "), wait)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
