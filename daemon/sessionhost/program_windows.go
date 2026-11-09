//go:build windows

package sessionhost

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/platform"
)

// conptyProgram is a program in a Windows pseudo console. It and every
// process it starts run in one Job: Kill ends the tree.
type conptyProgram struct {
	pc      *platform.PseudoConsole
	command string

	hangupOnce sync.Once
}

func startProgram(path string, argv, env []string, dir string, cols, rows int) (program, error) {
	resolved := append([]string{path}, argv[1:]...)
	pc, err := platform.StartPseudoConsole(platform.ConsoleSpec{Argv: resolved, Env: env, Dir: dir, Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	return &conptyProgram{pc: pc, command: strings.TrimSuffix(name, filepath.Ext(name))}, nil
}

func (p *conptyProgram) Read(b []byte) (int, error)  { return p.pc.Read(b) }
func (p *conptyProgram) Write(b []byte) (int, error) { return p.pc.Write(b) }
func (p *conptyProgram) Resize(cols, rows int) error { return p.pc.Resize(cols, rows) }
func (p *conptyProgram) PID() int                    { return p.pc.PID() }
func (p *conptyProgram) Close() error                { return p.pc.Close() }

// Cwd is unknown here; the Session falls back to the directory the program
// reports with OSC 7, then to the one it started in.
func (p *conptyProgram) Cwd() string { return "" }

// Command names what the Session runs, as a Unix Session reports its
// foreground process: the oldest process in its Job other than mewla
// itself (`mewla sh` starts the agent and waits for it), which is the agent
// rather than a tool it runs for a moment. The program's own name when that
// cannot be read.
func (p *conptyProgram) Command() string {
	pids, err := p.pc.PIDs()
	if err != nil {
		return p.command
	}
	name, oldest := p.command, time.Time{}
	for _, pid := range pids {
		image, err := platform.ProcessImageName(pid)
		if err != nil {
			continue
		}
		base := filepath.Base(image)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		if strings.EqualFold(base, "mewla") || strings.EqualFold(base, "conhost") {
			continue
		}
		started, err := platform.ProcessStartTime(pid)
		if err != nil || (!oldest.IsZero() && !started.Before(oldest)) {
			continue
		}
		name, oldest = base, started
	}
	return name
}

func (p *conptyProgram) Wait() (int, string) {
	code, err := p.pc.Wait()
	if err != nil {
		return -1, ""
	}
	return code, ""
}

// Signal maps the Unix signals onto a console: interrupt is Ctrl+C as
// typed; hangup and terminate close the console, which sends the programs
// in it CTRL_CLOSE_EVENT; kill ends the Job.
func (p *conptyProgram) Signal(name string) error {
	switch name {
	case SignalInterrupt:
		return p.pc.Interrupt()
	case SignalHangup, SignalTerminate:
		p.hangupOnce.Do(p.pc.Hangup)
		return nil
	case SignalKill:
		return p.pc.Kill()
	}
	return fmt.Errorf("unknown signal %q", name)
}

// lookPath resolves argv[0] against the Session's PATH and PATHEXT, not the
// host's.
func lookPath(name string, env []string) (string, error) {
	path, pathext := "", ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(key) {
		case "PATH":
			path = value
		case "PATHEXT":
			pathext = value
		}
	}
	found, err := platform.LookPathIn(name, path, pathext)
	if err != nil {
		return "", fmt.Errorf("%s: not found in the Session's PATH", name)
	}
	if !filepath.IsAbs(found) {
		if abs, err := filepath.Abs(found); err == nil {
			found = abs
		}
	}
	return found, nil
}
