//go:build windows

package launchsh

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	code, _ = Run(context.Background(), `no-such-program-mewla`, Options{Env: os.Environ(), Dir: dir, Stdout: &out, Stderr: &out})
	if code != 127 {
		t.Fatalf("a missing program = %d; %q", code, out.String())
	}
}
