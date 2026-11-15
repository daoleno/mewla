//go:build linux

package keeper

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Executable is the program a keeper process runs as: this mewla.
var Executable = os.Executable

// inProcess runs a keeper as part of this process instead: a test binary
// cannot be started as mewla. Its program is still a process of its own.
var inProcess = testing.Testing()

// logLimit bounds a keeper's log: past it, the log starts again.
const logLimit = 256 << 10

// Spawn starts a keeper for spec and waits until it listens. A program that
// cannot start says why (the last of its log).
func Spawn(spec Spec) error {
	if len(spec.Argv) == 0 || spec.Socket == "" {
		return errors.New("keeper: no program or socket")
	}
	dir := filepath.Dir(spec.Socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := spec.encode()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "keeper-spec-*.json")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	exited := make(chan struct{})
	if inProcess {
		go func() {
			defer close(exited)
			_ = run(f.Name())
		}()
	} else {
		exe, err := Executable()
		if err != nil {
			_ = os.Remove(f.Name())
			return err
		}
		cmd := exec.Command(exe)
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, SpecEnv+"=") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, SpecEnv+"="+f.Name())
		// A session of its own: no signal for the daemon's process group or
		// terminal reaches it, and it does not end with the daemon.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			_ = os.Remove(f.Name())
			return err
		}
		go func() { _ = cmd.Wait(); close(exited) }()
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("unix", spec.Socket, time.Second); err == nil {
			_ = c.Close()
			return nil
		}
		select {
		case <-exited:
			return fmt.Errorf("it ended at once: %s", Tail(spec.Log))
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("it did not start in time: %s", Tail(spec.Log))
}

// Tail is the last lines of a keeper's log, as one short string.
func Tail(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "no log"
	}
	text := strings.TrimSpace(strings.ToValidUTF8(string(raw), ""))
	lines := strings.Split(text, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	text = strings.Join(lines, " | ")
	if len(text) > 600 {
		text = text[len(text)-600:]
	}
	return text
}

// Conn is the daemon's attachment to a keeper. For a CDP keeper it carries
// Chrome's debugging protocol; otherwise nothing, and its end means the
// program ended.
type Conn struct {
	net.Conn
	r *bufio.Reader
	// Pid is the program's process ID.
	Pid int
}

func (c *Conn) Read(p []byte) (int, error) { return c.r.Read(p) }

// Attach connects to the keeper at socket.
func Attach(socket string) (*Conn, error) {
	c, err := net.DialTimeout("unix", socket, 3*time.Second)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	if _, err := io.WriteString(c, "attach\n"); err != nil {
		c.Close()
		return nil, err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		c.Close()
		return nil, err
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(line), "ok "))
	if err != nil || !strings.HasPrefix(line, "ok ") {
		c.Close()
		return nil, fmt.Errorf("keeper: unexpected answer %q", strings.TrimSpace(line))
	}
	_ = c.SetDeadline(time.Time{})
	return &Conn{Conn: c, r: r, Pid: pid}, nil
}

// Stop asks the keeper at socket to stop its program, and waits until it has.
func Stop(socket string) error {
	c, err := net.DialTimeout("unix", socket, 3*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := io.WriteString(c, "stop\n"); err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, c)
	return err
}

// Main is the keeper process: it reads its Spec from the file SpecEnv names.
func Main() error {
	// Asked to end (a person's kill, a session logging out): the program
	// stops with its keeper, as Stop would, not left without one.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	signals = quit
	return run(os.Getenv(SpecEnv))
}

// signals ends a keeper process the way Stop does; nil when the keeper runs
// inside a test.
var signals <-chan os.Signal

type keeper struct {
	spec Spec
	log  *cappedLog
	cmd  *exec.Cmd

	mu       sync.Mutex
	attached int
	left     time.Time // when the last client left
	current  net.Conn  // the CDP client
	toChrome *os.File

	stopping chan net.Conn
	exited   chan struct{}
}

func run(specPath string) error {
	raw, err := os.ReadFile(specPath)
	_ = os.Remove(specPath)
	if err != nil {
		return err
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return err
	}
	log := openLog(spec.Log)
	k := &keeper{spec: spec, log: log, left: time.Now(), stopping: make(chan net.Conn, 1), exited: make(chan struct{})}
	if spec.Lock != "" {
		lock, err := os.OpenFile(spec.Lock, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			k.logf("keeper: lock: %v", err)
			return err
		}
		defer lock.Close()
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			k.logf("keeper: %s is held by another runtime", spec.Lock)
			return err
		}
	}
	_ = os.Remove(spec.Socket)
	ln, err := net.Listen("unix", spec.Socket)
	if err != nil {
		k.logf("keeper: listen: %v", err)
		return err
	}
	defer ln.Close()
	if err := os.Chmod(spec.Socket, 0o600); err != nil {
		return err
	}
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = append([]string(nil), spec.Env...)
	if spec.OwnerEnv != "" {
		cmd.Env = append(cmd.Env, spec.OwnerEnv+"="+strconv.Itoa(os.Getpid()))
	}
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var fromChrome *os.File
	if spec.CDP {
		toR, toW, err := os.Pipe()
		if err != nil {
			return err
		}
		fromR, fromW, err := os.Pipe()
		if err != nil {
			return err
		}
		cmd.ExtraFiles = []*os.File{toR, fromW}
		defer toR.Close()
		defer fromW.Close()
		k.toChrome, fromChrome = toW, fromR
	}
	if err := cmd.Start(); err != nil {
		k.logf("keeper: start %s: %v", spec.Argv[0], err)
		k.cleanup()
		return err
	}
	k.cmd = cmd
	for _, f := range cmd.ExtraFiles {
		_ = f.Close()
	}
	go func() { _ = cmd.Wait(); close(k.exited) }()
	if fromChrome != nil {
		go k.fromChrome(fromChrome)
	}
	go k.accept(ln)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-k.exited:
			k.logf("keeper: %s ended (%v)", filepath.Base(spec.Argv[0]), cmd.ProcessState)
			k.sweep()
			k.cleanup()
			return nil
		case sig := <-signals:
			k.logf("keeper: %v: stopping %s", sig, filepath.Base(spec.Argv[0]))
			k.stop()
			k.cleanup()
			return nil
		case reply := <-k.stopping:
			k.stop()
			k.cleanup()
			if reply != nil {
				_ = reply.Close()
			}
			return nil
		case <-tick.C:
			k.mu.Lock()
			idle := k.attached == 0 && time.Since(k.left) >= spec.Linger
			k.mu.Unlock()
			if idle {
				k.logf("keeper: nobody attached for %s: stopping %s", spec.Linger, filepath.Base(spec.Argv[0]))
				k.stop()
				k.cleanup()
				return nil
			}
		}
	}
}

func (k *keeper) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(k.log, time.Now().Format("2006/01/02 15:04:05 ")+format+"\n", args...)
}

func (k *keeper) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go k.serve(c)
	}
}

func (k *keeper) serve(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	if err != nil {
		c.Close()
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	switch strings.TrimSpace(line) {
	case "stop":
		select {
		case k.stopping <- c:
		default:
			c.Close()
		}
	case "attach":
		k.mu.Lock()
		k.attached++
		old := k.current
		if k.spec.CDP {
			k.current = c
		}
		k.mu.Unlock()
		if old != nil {
			_ = old.Close() // a newer daemon took over
		}
		if _, err := fmt.Fprintf(c, "ok %d\n", k.cmd.Process.Pid); err == nil {
			if k.spec.CDP {
				_, _ = io.Copy(k.toChrome, r)
			} else {
				_, _ = io.Copy(io.Discard, r)
			}
		}
		k.mu.Lock()
		k.attached--
		k.left = time.Now()
		if k.current == c {
			k.current = nil
		}
		k.mu.Unlock()
		c.Close()
	default:
		c.Close()
	}
}

// fromChrome passes Chrome's messages to the attached client and drops them
// while nobody is attached: Chrome must never block on a full pipe.
func (k *keeper) fromChrome(f *os.File) {
	r := bufio.NewReader(f)
	for {
		msg, err := r.ReadBytes(0)
		if err != nil {
			return
		}
		k.mu.Lock()
		c := k.current
		k.mu.Unlock()
		if c != nil {
			_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := c.Write(msg); err != nil {
				_ = c.Close()
			}
		}
	}
}

// stop ends the program: Chrome is asked to close first, so it saves the
// profile (the agents' sign-ins); then its whole process group is ended.
func (k *keeper) stop() {
	k.mu.Lock()
	c := k.current
	k.current = nil
	k.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
	if k.spec.CDP && k.toChrome != nil {
		_, _ = k.toChrome.Write([]byte(`{"id":2147483000,"method":"Browser.close"}` + "\x00"))
		select {
		case <-k.exited:
		case <-time.After(8 * time.Second):
		}
	}
	pid := k.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-k.exited:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-k.exited
	}
	k.sweep()
}

// sweep ends what is left of the program's process group (Chrome's helpers).
func (k *keeper) sweep() {
	if k.cmd != nil && k.cmd.Process != nil {
		_ = syscall.Kill(-k.cmd.Process.Pid, syscall.SIGTERM)
	}
}

func (k *keeper) cleanup() {
	for _, p := range k.spec.Cleanup {
		_ = os.RemoveAll(p)
	}
	_ = os.Remove(k.spec.Socket)
}

// cappedLog is a log file that starts again past logLimit.
type cappedLog struct {
	mu sync.Mutex
	f  *os.File
	n  int64
}

func openLog(path string) *cappedLog {
	l := &cappedLog{}
	if path == "" {
		return l
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return l
	}
	l.f = f
	if info, err := f.Stat(); err == nil {
		l.n = info.Size()
	}
	return l
}

func (l *cappedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil
	}
	if l.n+int64(len(p)) > logLimit {
		_ = l.f.Truncate(0)
		l.n = 0
	}
	n, err := l.f.Write(p)
	l.n += int64(n)
	return n, err
}
