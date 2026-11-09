//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ConsoleSpec is a program to run in a pseudo console.
type ConsoleSpec struct {
	// Argv is the program and its arguments, never a shell string.
	Argv []string
	// Env is the full environment; nil means this process's.
	Env        []string
	Dir        string
	Cols, Rows int
}

// PseudoConsole is a program running in a Windows pseudo console (ConPTY),
// the Windows counterpart of a Unix pty: the program sees a console, and
// its screen arrives here as VT bytes. The program and every process it
// starts run in one Job, so Kill ends the whole tree.
type PseudoConsole struct {
	in, out *os.File
	hpc     windows.Handle
	process windows.Handle
	pid     int
	job     *Job

	closeOnce sync.Once
	waitOnce  sync.Once
	lastRead  atomic.Int64 // unix nanos of the last read that returned bytes
	exitCode  uint32
	waitErr   error
}

const procThreadAttributePseudoconsole = 0x00020016

const (
	outputQuiet = 300 * time.Millisecond
	outputDrain = 3 * time.Second
)

// ResolveProgram finds argv[0] on PATH the way a Windows shell would and
// returns the executable to start and its command line. A .cmd or .bat shim
// (what npm installs) runs through cmd.exe, since CreateProcess starts only
// executables; see shimCommandLine for what its arguments may hold.
func ResolveProgram(argv []string, path string) (application, commandLine string, err error) {
	if len(argv) == 0 || argv[0] == "" {
		return "", "", errors.New("no program to run")
	}
	program := argv[0]
	if !filepath.IsAbs(program) {
		found, err := LookPathIn(program, path, os.Getenv("PATHEXT"))
		if err != nil {
			return "", "", err
		}
		program = found
	}
	resolved := append([]string{program}, argv[1:]...)
	switch strings.ToLower(filepath.Ext(program)) {
	case ".cmd", ".bat":
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		}
		line, err := shimCommandLine(resolved)
		if err != nil {
			return "", "", err
		}
		return comspec, quoteArg(comspec) + ` /d /s /c "` + line + `"`, nil
	}
	return program, QuoteArgs(resolved), nil
}

// shimCommandLine quotes every argument for cmd.exe. cmd.exe expands %VAR%
// even inside quotes and has no escape for a quote within one, so an
// argument holding %, ", CR or LF could run commands of its own: such an
// argument is refused rather than passed to a batch file.
func shimCommandLine(argv []string) (string, error) {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if strings.ContainsAny(arg, "%\"\r\n\x00") {
			return "", fmt.Errorf("argument %d holds a character a .cmd program cannot be given safely (%%, \", CR or LF)", i)
		}
		quoted[i] = `"` + arg + `"`
	}
	return strings.Join(quoted, " "), nil
}

// LookPathIn finds program in path (a PATH value), trying each PATHEXT
// extension when program has none. An empty path searches this process's
// PATH.
func LookPathIn(program, path, pathext string) (string, error) {
	if path == "" {
		return exec.LookPath(program)
	}
	if strings.ContainsAny(program, `\/`) {
		if info, err := os.Stat(program); err == nil && info.Mode().IsRegular() {
			return program, nil
		}
		return "", fmt.Errorf("%s: %w", program, exec.ErrNotFound)
	}
	var exts []string
	for _, ext := range strings.Split(strings.ToLower(pathext), ";") {
		if ext = strings.TrimSpace(ext); ext != "" {
			exts = append(exts, ext)
		}
	}
	if len(exts) == 0 {
		exts = []string{".com", ".exe", ".bat", ".cmd"}
	}
	candidates := []string{program}
	if filepath.Ext(program) == "" {
		candidates = nil
		for _, ext := range exts {
			candidates = append(candidates, program+ext)
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, name := range candidates {
			full := filepath.Join(dir, name)
			if info, err := os.Stat(full); err == nil && info.Mode().IsRegular() {
				return full, nil
			}
		}
	}
	return "", fmt.Errorf("%s: %w", program, exec.ErrNotFound)
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// environmentBlock is env as CreateProcess takes it: UTF-16 KEY=value
// strings, each NUL-terminated, then one more NUL.
func environmentBlock(env []string) *uint16 {
	var block []uint16
	for _, kv := range env {
		if strings.IndexByte(kv, 0) >= 0 || !strings.Contains(kv, "=") {
			continue
		}
		block = append(block, utf16.Encode([]rune(kv))...)
		block = append(block, 0)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return &block[0]
}

// StartPseudoConsole starts spec's program in a new pseudo console. The
// process is created suspended, put in its Job, then resumed, so nothing it
// starts can escape the Job.
func StartPseudoConsole(spec ConsoleSpec) (_ *PseudoConsole, err error) {
	env := spec.Env
	if env == nil {
		env = os.Environ()
	}
	program, line, err := ResolveProgram(spec.Argv, envValue(env, "PATH"))
	if err != nil {
		return nil, err
	}
	cols, rows := spec.Cols, spec.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("console input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("console output pipe: %w", err)
	}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &hpc); err != nil {
		for _, h := range []windows.Handle{inRead, inWrite, outRead, outWrite} {
			windows.CloseHandle(h)
		}
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	// conhost holds its own copies of these ends.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	pc := &PseudoConsole{
		in:  os.NewFile(uintptr(inWrite), "conpty-in"),
		out: os.NewFile(uintptr(outRead), "conpty-out"),
		hpc: hpc,
	}
	defer func() {
		if err != nil {
			pc.Close()
		}
	}()

	// The attribute's value is the HPCON itself, not a pointer to it.
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	if err := attrs.Update(procThreadAttributePseudoconsole, *(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil {
		return nil, fmt.Errorf("attach the pseudo console: %w", err)
	}
	startup := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	startup.Cb = uint32(unsafe.Sizeof(*startup))
	// Zero std handles with STARTF_USESTDHANDLES: the program gets the
	// pseudo console's, not this process's.
	startup.Flags = windows.STARTF_USESTDHANDLES
	commandLine, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if spec.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(spec.Dir); err != nil {
			return nil, err
		}
	}
	application, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return nil, err
	}
	job, err := NewJob()
	if err != nil {
		return nil, err
	}
	pc.job = job
	var info windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(application, commandLine, nil, nil, false, flags,
		environmentBlock(env), dir, &startup.StartupInfo, &info); err != nil {
		return nil, fmt.Errorf("start %s: %w", program, err)
	}
	pc.process, pc.pid = info.Process, int(info.ProcessId)
	if err := job.AssignHandle(info.Process); err != nil {
		windows.TerminateProcess(info.Process, 1)
		windows.CloseHandle(info.Thread)
		return nil, err
	}
	if _, err := windows.ResumeThread(info.Thread); err != nil {
		windows.TerminateProcess(info.Process, 1)
		windows.CloseHandle(info.Thread)
		return nil, fmt.Errorf("resume %s: %w", program, err)
	}
	windows.CloseHandle(info.Thread)
	return pc, nil
}

// PID is the program's process id.
func (p *PseudoConsole) PID() int { return p.pid }

// Read reads the console's output: VT bytes as conhost renders them.
func (p *PseudoConsole) Read(b []byte) (int, error) {
	n, err := p.out.Read(b)
	if n > 0 {
		p.lastRead.Store(time.Now().UnixNano())
	}
	return n, err
}

// Write types into the console.
func (p *PseudoConsole) Write(b []byte) (int, error) { return p.in.Write(b) }

// Resize changes the console's size.
func (p *PseudoConsole) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Wait waits for the program to exit and returns its exit code. When it
// has, the pseudo console is closed so Read reaches the end of the output.
func (p *PseudoConsole) Wait() (int, error) {
	p.waitOnce.Do(func() {
		if _, err := windows.WaitForSingleObject(p.process, windows.INFINITE); err != nil {
			p.waitErr = err
			return
		}
		if err := windows.GetExitCodeProcess(p.process, &p.exitCode); err != nil {
			p.waitErr = err
		}
		// conhost keeps the output pipe open until the console closes, and
		// may still be rendering the program's last output: let a reader
		// drain it (until output pauses, at most outputDrain) before closing.
		exited := time.Now()
		deadline := exited.Add(outputDrain)
		for time.Now().Before(deadline) {
			last := time.Unix(0, p.lastRead.Load())
			if last.Before(exited) {
				last = exited
			}
			if time.Since(last) >= outputQuiet {
				break
			}
			time.Sleep(outputQuiet / 6)
		}
		p.closeConsole()
	})
	return int(p.exitCode), p.waitErr
}

// Kill ends the program and every process it started.
func (p *PseudoConsole) Kill() error {
	if p.job == nil {
		return errors.New("no job")
	}
	return p.job.Terminate(1)
}

// Interrupt sends Ctrl+C, as a person typing it would.
func (p *PseudoConsole) Interrupt() error {
	_, err := p.Write([]byte{0x03})
	return err
}

// Hangup closes the console, as closing a terminal window does: the
// programs attached to it get CTRL_CLOSE_EVENT and a few seconds to exit.
func (p *PseudoConsole) Hangup() { p.closeConsole() }

func (p *PseudoConsole) closeConsole() {
	p.closeOnce.Do(func() {
		if p.hpc != 0 {
			windows.ClosePseudoConsole(p.hpc)
		}
	})
}

// Close ends the program and releases the console, pipes and Job.
func (p *PseudoConsole) Close() error {
	if p.job != nil {
		_ = p.job.Terminate(1)
	}
	p.closeConsole()
	if p.in != nil {
		p.in.Close()
	}
	if p.out != nil {
		p.out.Close()
	}
	if p.process != 0 {
		windows.CloseHandle(p.process)
		p.process = 0
	}
	if p.job != nil {
		p.job.Close()
	}
	return nil
}
