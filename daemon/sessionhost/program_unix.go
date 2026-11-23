//go:build linux || darwin

package sessionhost

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

// ptyProgram is a program on a Unix pty. It leads its own session with the
// pty as its controlling terminal, so its process group is the Session's.
type ptyProgram struct {
	cmd  *exec.Cmd
	ptmx *os.File
}

// startProgram starts the program on a new pty. With place, the child is
// first this binary (ScopeExecArg), held on a pipe until place(pid) has put
// it where it belongs, and only then becomes the program, same pid: nothing
// the program starts can run before it is placed.
func startProgram(path string, argv, env []string, dir string, cols, rows int, place func(pid int)) (program, error) {
	if place != nil {
		return startPlacedProgram(path, argv, env, dir, cols, rows, place)
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Args = append([]string(nil), argv...)
	cmd.Env = env
	cmd.Dir = dir
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &ptyProgram{cmd: cmd, ptmx: ptmx}, nil
}

func startPlacedProgram(path string, argv, env []string, dir string, cols, rows int, place func(pid int)) (program, error) {
	self := "/proc/self/exe"
	if runtime.GOOS != "linux" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		self = exe
	}
	gate, release, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self)
	cmd.Args = append([]string{argv[0], ScopeExecArg, path}, argv...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.ExtraFiles = []*os.File{gate}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	_ = gate.Close()
	if err != nil {
		_ = release.Close()
		return nil, err
	}
	place(cmd.Process.Pid)
	_, _ = release.Write([]byte{1})
	_ = release.Close()
	return &ptyProgram{cmd: cmd, ptmx: ptmx}, nil
}

// ScopeExecArg is the first argument of a program the host holds until it is
// placed (startPlacedProgram); RunScopeExec handles it.
const ScopeExecArg = "__mewla-scope-exec"

// RunScopeExec returns unless this process is such a held program: then it
// waits for the host's word on fd 3 and becomes the program (same pid). Every
// binary that runs a host with SessionScopes calls it first thing in main.
func RunScopeExec() {
	if len(os.Args) < 4 || os.Args[1] != ScopeExecArg {
		return
	}
	gate := os.NewFile(3, "scope-gate")
	var word [1]byte
	_, _ = gate.Read(word[:]) // the word, or EOF if the host is gone: start either way
	_ = gate.Close()
	path, argv := os.Args[2], os.Args[3:]
	err := syscall.Exec(path, argv, os.Environ())
	fmt.Fprintf(os.Stderr, "start %s: %v\n", argv[0], err)
	os.Exit(127)
}

func (p *ptyProgram) Read(b []byte) (int, error)  { return p.ptmx.Read(b) }
func (p *ptyProgram) Write(b []byte) (int, error) { return p.ptmx.Write(b) }
func (p *ptyProgram) PID() int                    { return p.cmd.Process.Pid }
func (p *ptyProgram) Close() error                { return p.ptmx.Close() }
func (p *ptyProgram) Cwd() string                 { return processCwd(p.ptmx, p.PID()) }
func (p *ptyProgram) Command() string             { return processCommand(p.ptmx, p.PID()) }

func (p *ptyProgram) Resize(cols, rows int) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *ptyProgram) Wait() (int, string) {
	err := p.cmd.Wait()
	return exitStatus(p.cmd.ProcessState, err)
}

// Signal sends the signal to the program's whole process group.
func (p *ptyProgram) Signal(name string) error {
	sig, ok := map[string]syscall.Signal{
		SignalInterrupt: syscall.SIGINT,
		SignalTerminate: syscall.SIGTERM,
		SignalHangup:    syscall.SIGHUP,
		SignalKill:      syscall.SIGKILL,
	}[name]
	if !ok {
		return fmt.Errorf("unknown signal %q", name)
	}
	if err := syscall.Kill(-p.PID(), sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func exitStatus(state *os.ProcessState, err error) (int, string) {
	if state == nil {
		return -1, ""
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal()), signalName(status.Signal())
	}
	return state.ExitCode(), ""
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGHUP:
		return SignalHangup
	case syscall.SIGINT:
		return SignalInterrupt
	case syscall.SIGTERM:
		return SignalTerminate
	case syscall.SIGKILL:
		return SignalKill
	}
	return sig.String()
}

// lookPath resolves argv[0] against the Session's PATH, not the host's.
func lookPath(name string, env []string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	path := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s: not found in the Session's PATH", name)
}
