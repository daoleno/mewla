package agentuse

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The extension's service worker with a stand-in chrome: the zoom a page laid out for the phone keeps (a refit
// at the same scale sets nothing, a navigation's reset is undone), putting the window back, and its own update.
func TestExtensionBackground(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--no-warnings", "testdata/background_test.mjs", "extension/background.js")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil || strings.TrimSpace(stdout.String()) != "ok" {
		t.Fatalf("background_test.mjs: %v\n%s%s", err, stdout.String(), stderr.String())
	}
}
