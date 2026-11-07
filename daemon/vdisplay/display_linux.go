//go:build linux

package vdisplay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Capability says whether this computer can run a virtual display.
func Capability() error {
	for _, tool := range []string{"Xvnc", "xauth"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is not installed (TigerVNC provides Xvnc)", tool)
		}
	}
	return nil
}

// Display is one private X display backed by Xvnc. Nothing listens on TCP:
// the picture is served on a Unix socket only its owner can open, and X
// clients need the cookie in the display's own auth file.
type Display struct {
	Dir    string // holds the socket and the auth file, mode 0700
	Number int
	Width  int
	Height int

	cmd  *exec.Cmd
	done chan struct{}

	mu   sync.Mutex
	apps []*exec.Cmd
}

// SocketPath is the private RFB socket.
func (d *Display) SocketPath() string { return filepath.Join(d.Dir, "rfb.sock") }

// AuthPath is the display's X authority file.
func (d *Display) AuthPath() string { return filepath.Join(d.Dir, "xauth") }

// Start brings up a display in dir (created 0700) at the given size. It picks
// a free display number itself and tries the next one if another program takes
// it first.
func Start(ctx context.Context, dir string, width, height int) (*Display, error) {
	if err := Capability(); err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		width, height = 1280, 800
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	var last error
	for _, number := range freeNumbers() {
		d, err := startOn(ctx, dir, number, width, height)
		if err == nil {
			return d, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	if last == nil {
		last = errors.New("no free display number")
	}
	return nil, last
}

// freeNumbers lists display numbers nothing is using, starting somewhere
// random so two Mewla daemons don't race for the same one.
func freeNumbers() []int {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	start := 100 + int(b[0])%100
	var out []int
	for i := 0; i < 100 && len(out) < 8; i++ {
		n := 100 + (start-100+i)%100
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n)); err == nil {
			continue
		}
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X%d-lock", n)); err == nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func startOn(ctx context.Context, dir string, number, width, height int) (*Display, error) {
	d := &Display{Dir: dir, Number: number, Width: width, Height: height, done: make(chan struct{})}
	_ = os.Remove(d.SocketPath())
	_ = os.Remove(d.AuthPath())
	if err := writeCookieFor(d.AuthPath(), number); err != nil {
		return nil, err
	}
	cmd := exec.Command("Xvnc", fmt.Sprintf(":%d", number),
		"-auth", d.AuthPath(),
		"-rfbport", "-1",
		"-rfbunixpath", d.SocketPath(), "-rfbunixmode", "0600",
		"-SecurityTypes", "None",
		"-nolisten", "tcp",
		"-AlwaysShared",
		"-geometry", fmt.Sprintf("%dx%d", width, height), "-depth", "24")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	d.cmd = cmd
	go func() { _ = cmd.Wait(); close(d.done) }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-d.done:
			_ = os.Remove(d.AuthPath())
			return nil, errors.New("the display exited while starting")
		case <-ctx.Done():
			d.Stop()
			return nil, ctx.Err()
		default:
		}
		if c, err := d.Dial(); err == nil {
			_ = c.Close()
			return d, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.Stop()
	return nil, errors.New("the display took too long to start")
}

func writeCookieFor(path string, number int) error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	cmd := exec.Command("xauth", "-q", "-f", path, "add", fmt.Sprintf(":%d", number), ".", hex.EncodeToString(b))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("xauth: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return os.Chmod(path, 0o600)
}

// Dial opens an RFB connection to the display.
func (d *Display) Dial() (net.Conn, error) {
	return net.DialTimeout("unix", d.SocketPath(), 3*time.Second)
}

// Client connects the agent's own RFB client.
func (d *Display) Client() (*Client, error) {
	conn, err := d.Dial()
	if err != nil {
		return nil, err
	}
	c, err := Connect(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// Launch starts an app on the display. Qt and GTK apps are told to use X even
// when the computer's own session is Wayland.
func (d *Display) Launch(dir string, argv ...string) (*App, error) {
	if len(argv) == 0 {
		return nil, errors.New("no program to start")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, fmt.Errorf("%s is not installed", argv[0])
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(filteredEnv(),
		"DISPLAY="+fmt.Sprintf(":%d", d.Number),
		"XAUTHORITY="+d.AuthPath(),
		"QT_QPA_PLATFORM=xcb",
		"GDK_BACKEND=x11",
		"MOZ_ENABLE_WAYLAND=0",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	app := &App{Cmd: cmd, Done: make(chan struct{})}
	d.mu.Lock()
	d.apps = append(d.apps, cmd)
	d.mu.Unlock()
	go func() { _ = cmd.Wait(); app.Code = cmd.ProcessState.ExitCode(); close(app.Done) }()
	return app, nil
}

// App is a program running on the display. Code is valid once Done is closed.
type App struct {
	Cmd  *exec.Cmd
	Done chan struct{}
	Code int
}

// filteredEnv is the environment without the person's own display, so an app
// can only reach the virtual one.
func filteredEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "DISPLAY="), strings.HasPrefix(kv, "WAYLAND_DISPLAY="),
			strings.HasPrefix(kv, "XAUTHORITY="), strings.HasPrefix(kv, "QT_QPA_PLATFORM="),
			strings.HasPrefix(kv, "GDK_BACKEND="):
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Alive reports whether the display is still up.
func (d *Display) Alive() bool {
	select {
	case <-d.done:
		return false
	default:
		return true
	}
}

// Stop ends the apps and the display and removes its files.
func (d *Display) Stop() {
	d.mu.Lock()
	apps := d.apps
	d.apps = nil
	d.mu.Unlock()
	for _, a := range apps {
		if a.Process != nil {
			_ = syscall.Kill(-a.Process.Pid, syscall.SIGTERM)
		}
	}
	if d.cmd != nil && d.cmd.Process != nil {
		_ = syscall.Kill(-d.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-d.done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-d.cmd.Process.Pid, syscall.SIGKILL)
			<-d.done
		}
	}
	_ = os.Remove(d.SocketPath())
	_ = os.Remove(d.AuthPath())
}
