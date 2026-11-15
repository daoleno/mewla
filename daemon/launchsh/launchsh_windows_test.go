//go:build windows

package launchsh

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/work"
)

// The real starter: programs and npm's .cmd shims found on the script's PATH.
func TestStartsProgramsAndShimsFromTheScriptsPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.cmd"), []byte("@echo shim got [%~1] [%~2] in %MEWLA_SESSION_ID%\r\n@exit /b 4\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "MEWLA_SESSION_ID=mh-1")
	var out bytes.Buffer
	code, err := Run(context.Background(), `PATH="`+filepath.ToSlash(dir)+`;$PATH"; export PATH; exec '/bin/sh' -l -c 'agent "two words" x'`,
		Options{Env: env, Dir: dir, Stdout: &out, Stderr: &out})
	if err != nil || code != 4 {
		t.Fatalf("Run = %d, %v; output %q", code, err, out.String())
	}
	if !strings.Contains(out.String(), "shim got [two words] [x] in mh-1") {
		t.Fatalf("output = %q", out.String())
	}
	out.Reset()
	code, err = Run(context.Background(), `cmd.exe /d /c "exit 6"`, Options{Env: os.Environ(), Dir: dir, Stdout: &out, Stderr: &out})
	if err != nil || code != 6 {
		t.Fatalf("cmd exit = %d, %v; %q", code, err, out.String())
	}
	// A Shell Session's login shell is PowerShell (here without a console, so
	// it shows its prompt and ends at the end of its input).
	out.Reset()
	code, err = Run(context.Background(), `exec '/bin/sh' -i -l -c 'exec '\''/bin/sh'\'' -i -l'`, Options{Env: os.Environ(), Dir: dir, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out})
	if err != nil || code != 0 || !strings.HasPrefix(out.String(), "PS ") {
		t.Fatalf("login shell = %d, %v; %q", code, err, out.String())
	}
	code, _ = Run(context.Background(), `no-such-program-mewla`, Options{Env: os.Environ(), Dir: dir, Stdout: &out, Stderr: &out})
	if code != 127 {
		t.Fatalf("a missing program = %d; %q", code, out.String())
	}
}

// A Shell Session's login shell is PowerShell 7 when it is installed (here a
// copy of cmd.exe named pwsh.exe stands in for it), Windows PowerShell
// otherwise.
func TestLoginShellPrefersPowerShell7(t *testing.T) {
	dir := t.TempDir()
	cmdExe, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pwsh.exe"), cmdExe, 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, entry := range os.Environ() {
		if key, _, _ := strings.Cut(entry, "="); !strings.EqualFold(key, "PATH") {
			env = append(env, entry)
		}
	}
	env = append(env, "PATH="+dir+";"+os.Getenv("PATH"))
	var out bytes.Buffer
	code, err := Run(context.Background(), `exec '/bin/sh' -i -l`, Options{Env: env, Dir: dir, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &out})
	// The stand-in prints cmd's copyright line; PowerShell with -NoLogo prints
	// only its prompt.
	if err != nil || code != 0 || !strings.Contains(out.String(), "Microsoft Corporation") || strings.HasPrefix(out.String(), "PS ") {
		t.Fatalf("login shell with pwsh on PATH = %d, %v; %q (want the pwsh.exe stand-in)", code, err, out.String())
	}
}

// TestMain lets this test binary stand in for an agent: with
// MEWLA_TEST_ECHO_ARGV set it prints the arguments it was given, one JSON
// array, and exits.
func TestMain(m *testing.M) {
	if os.Getenv("MEWLA_TEST_ECHO_ARGV") == "1" {
		raw, _ := json.Marshal(os.Args[1:])
		os.Stdout.Write(raw)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// npm installs Claude Code as %APPDATA%\npm\claude.cmd. Brain's own launch
// carries --settings JSON, and a Worker's --mcp-config JSON; through cmd.exe
// those quotes cannot pass, and 0.4.6 refused them: Brain's Claude exited
// 127 before it drew a frame, every time (a Windows user's Brain never
// started). The shim's program now gets the command's arguments exactly.
func TestNpmClaudeGetsBrainsLaunchArgumentsExactly(t *testing.T) {
	npm := t.TempDir()
	bin := filepath.Join(npm, "node_modules", "@anthropic-ai", "claude-code", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "claude.exe"), self, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n" +
		`"%dp0%\node_modules\@anthropic-ai\claude-code\bin\claude.exe"   %*` + "\r\n"
	if err := os.WriteFile(filepath.Join(npm, "claude.cmd"), []byte(shim), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := `C:\Users\100% "me" & ^you\.mewla\brain\workspace\`
	command, err := work.EnsureClaudeSessionLaunchCommand(work.HardenClaudeCommand("claude") + " --add-dir " + shellquote.Word(workspace))
	if err != nil {
		t.Fatal(err)
	}
	command = work.WithAgentTools(command, `C:\Program Files\Mewla 100%\mewla.exe`, `C:\Users\同事\.mewla`, work.AgentTools{Chrome: true, Computer: true})
	want, ok := shellquote.Fields(command)
	if !ok || !strings.Contains(command, `--settings '{"`) || !strings.Contains(command, "--mcp-config") {
		t.Fatalf("not Brain's and a Worker's launch: %q", command)
	}
	env := append(os.Environ(), "MEWLA_TEST_ECHO_ARGV=1")
	var out, errOut bytes.Buffer
	code, err := Run(context.Background(), `PATH="`+filepath.ToSlash(npm)+`;$PATH"; export PATH; exec `+command, Options{Env: env, Dir: npm, Stdout: &out, Stderr: &errOut})
	if err != nil || code != 0 {
		t.Fatalf("Run = %d, %v; stderr %q", code, err, errOut.String())
	}
	var got []string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	if !slices.Equal(got, want[1:]) {
		t.Fatalf("claude got\n%q\nwant\n%q", got, want[1:])
	}
}
