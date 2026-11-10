//go:build linux

package vdisplay

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// The agent screen has an accessibility bus of its own: the bus AT-SPI uses,
// with its registry, private to the display. Apps opened on the display join
// it, so an agent can read their buttons and fields and act on them, without
// depending on (or switching on) accessibility in the person's own session.

// a11yConfigs are where distributions keep the accessibility bus's config.
var a11yConfigs = []string{
	"/usr/share/defaults/at-spi2/accessibility.conf",
	"/etc/at-spi2/accessibility.conf",
}

// registryPaths are where distributions keep the AT-SPI registry daemon.
var registryPaths = []string{
	"/usr/lib/at-spi2-registryd",
	"/usr/libexec/at-spi2-registryd",
	"/usr/lib/at-spi2-core/at-spi2-registryd",
	"/usr/lib/x86_64-linux-gnu/at-spi2-core/at-spi2-registryd",
	"/usr/lib/aarch64-linux-gnu/at-spi2-core/at-spi2-registryd",
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// errNoA11y means the computer lacks AT-SPI (at-spi2-core) or D-Bus, so apps
// on the agent screen can be seen and clicked but not read as elements.
var errNoA11y = errors.New("this computer has no AT-SPI accessibility bus (install at-spi2-core)")

// a11yBus is the display's accessibility bus and its registry.
type a11yBus struct {
	address  string
	bus      *exec.Cmd
	registry *exec.Cmd
}

// startA11y starts the display's accessibility bus in dir.
func startA11y(dir string) (*a11yBus, error) {
	config := firstExisting(a11yConfigs)
	registry := firstExisting(registryPaths)
	daemon, err := exec.LookPath("dbus-daemon")
	if config == "" || registry == "" || err != nil {
		return nil, errNoA11y
	}
	socket := filepath.Join(dir, "a11y")
	_ = os.Remove(socket)
	address := "unix:path=" + socket
	bus := exec.Command(daemon, "--config-file="+config, "--nofork", "--address="+address)
	bus.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := bus.Start(); err != nil {
		return nil, err
	}
	b := &a11yBus{address: address, bus: bus}
	go func() { _ = bus.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := net.DialTimeout("unix", socket, time.Second); err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			b.stop()
			return nil, errors.New("the accessibility bus did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	reg := exec.Command(registry)
	reg.Env = append(os.Environ(), "AT_SPI_BUS_ADDRESS="+address)
	reg.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := reg.Start(); err != nil {
		b.stop()
		return nil, err
	}
	b.registry = reg
	go func() { _ = reg.Wait() }()
	return b, nil
}

func (b *a11yBus) stop() {
	if b == nil {
		return
	}
	for _, cmd := range []*exec.Cmd{b.registry, b.bus} {
		if cmd != nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
	}
}

// a11yEnv is what an app on the display needs to join its accessibility bus.
// Qt apps join only when told to.
func (b *a11yBus) env() []string {
	if b == nil {
		return nil
	}
	return []string{"AT_SPI_BUS_ADDRESS=" + b.address, "QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1", "QT_ACCESSIBILITY=1"}
}

// A11yAddress is the display's accessibility bus, or "" when it has none.
func (d *Display) A11yAddress() string {
	if d.a11y == nil {
		return ""
	}
	return d.a11y.address
}
