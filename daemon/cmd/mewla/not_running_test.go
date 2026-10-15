package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/daoleno/mewla/daemon/control"
)

func TestCommandsSayPlainlyThatMewlaIsNotRunning(t *testing.T) {
	// Short: a Unix socket path has a length limit.
	dir, err := os.MkdirTemp("", "m")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	_, err = callControl(cliConfig{stateDir: dir}, control.Request{Type: "computers"})
	if !errors.Is(err, errMewlaNotRunning) || err.Error() != "Mewla isn't running here. Start it with: mewla -state-dir "+dir {
		t.Fatalf("not running: %v", err)
	}
	if got := (mewlaNotRunningError{stateDir: resolveCLIStateDir("")}).Error(); got != "Mewla isn't running here. Start it with: mewla" {
		t.Fatalf("default state dir: %q", got)
	}
	err = runComputersCommand([]string{"list", "--state-dir", filepath.Join(dir, "other")}, io.Discard)
	var notRunning mewlaNotRunningError
	if !errors.As(err, &notRunning) {
		t.Fatalf("computers list: %v", err)
	}
}
