//go:build !windows

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A CLI whose --version leaves a child holding its output must not hang the
// probe past its timeout: the daemon's shutdown, and so its update, waited on
// one for minutes (11-17).
func TestAProbeWhoseChildKeepsItsOutputOpenEndsAtItsTimeout(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "agent")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nsleep 30 &\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _ = env{}.defaultRunCommand(ctx, cli, "--version")
	if took := time.Since(started); took > probeWaitDelay+3*time.Second {
		t.Fatalf("the probe took %s; it must end by its timeout plus %s", took, probeWaitDelay)
	}
}
