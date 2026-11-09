//go:build windows

package platform

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type consoleOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *consoleOutput) collect(r io.Reader) {
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		o.mu.Lock()
		o.buf.Write(chunk[:n])
		o.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (o *consoleOutput) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		got := o.buf.String()
		o.mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	t.Fatalf("console never showed %q; it showed:\n%q", want, o.buf.String())
}

// A program in the pseudo console gets its argv as given, its environment
// and its directory, and its exit code comes back.
func TestPseudoConsoleRunsAProgramWithArgvEnvAndDir(t *testing.T) {
	dir := t.TempDir()
	env := append(os.Environ(), "MEWLA_CONPTY_PROBE=quoted value")
	pc, err := StartPseudoConsole(ConsoleSpec{
		Argv: []string{"cmd.exe", "/d", "/c", "echo probe:%MEWLA_CONPTY_PROBE%:& cd & exit 7"},
		Env:  env, Dir: dir, Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	var out consoleOutput
	go out.collect(pc)
	code, err := pc.Wait()
	if err != nil || code != 7 {
		t.Fatalf("exit = %d, %v; want 7", code, err)
	}
	out.waitFor(t, "probe:quoted value:")
	out.waitFor(t, strings.TrimSuffix(filepathBase(dir), `\`))
}

// Typing reaches an interactive program, resizing works, and Kill ends the
// program together with what it started.
func TestPseudoConsoleTypeResizeAndKillTheTree(t *testing.T) {
	pc, err := StartPseudoConsole(ConsoleSpec{Argv: []string{"cmd.exe", "/d", "/q"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	var out consoleOutput
	go out.collect(pc)
	if _, err := pc.Write([]byte("echo typed-%COMPUTERNAME%-ok\r")); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "typed-"+os.Getenv("COMPUTERNAME")+"-ok")
	if err := pc.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if _, err := pc.Write([]byte("mode con\r")); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "120")
	if _, err := pc.Write([]byte("ping -n 60 127.0.0.1\r")); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "Pinging")
	pids, err := pc.job.PIDs()
	if err != nil || len(pids) < 2 {
		t.Fatalf("job holds %v, %v; want cmd and ping", pids, err)
	}
	if err := pc.Kill(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { pc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after Kill")
	}
	for _, pid := range pids {
		for i := 0; i < 50 && ProcessAlive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if ProcessAlive(pid) {
			t.Fatalf("pid %d outlived Kill", pid)
		}
	}
}

func TestResolveProgramRunsShimsThroughCmd(t *testing.T) {
	dir := t.TempDir()
	shim := dir + `\agent.cmd`
	if err := os.WriteFile(shim, []byte("@echo shim %*\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, line, err := ResolveProgram([]string{"agent", "a b"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.ToLower(app), "cmd.exe") || !strings.HasSuffix(line, ` /d /s /c ""`+shim+`" "a b""`) {
		t.Fatalf("app = %q, line = %q", app, line)
	}
	for _, bad := range []string{"100%", `say "hi"`, "a\r\nb"} {
		if _, _, err := ResolveProgram([]string{"agent", bad}, dir); err == nil {
			t.Fatalf("argument %q was handed to a .cmd", bad)
		}
	}
	pc, err := StartPseudoConsole(ConsoleSpec{Argv: []string{"agent", "a b"}, Env: append(os.Environ(), "PATH="+dir+";"+os.Getenv("PATH"))})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	var out consoleOutput
	go out.collect(pc)
	if code, err := pc.Wait(); err != nil || code != 0 {
		t.Fatalf("exit = %d, %v", code, err)
	}
	out.waitFor(t, `shim "a b"`)
	if app, line, err := ResolveProgram([]string{"cmd.exe", `a "b"`}, ""); err != nil || !strings.HasSuffix(strings.ToLower(app), `\cmd.exe`) || !strings.HasSuffix(line, `"a \"b\""`) {
		t.Fatalf("an .exe keeps Go's quoting: %q %q %v", app, line, err)
	}
}

func filepathBase(path string) string {
	i := strings.LastIndex(path, `\`)
	return path[i+1:]
}
