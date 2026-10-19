package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/connections"
)

// --args-file must carry a tool's declared payload, such as Onlora's 4 MB
// base64 asset, not just the old 64 KiB bound.
func TestReadBoundedFileAcceptsLargeToolArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "args.json")
	payload := `{"data":"` + strings.Repeat("A", 5<<20) + `"}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := readBoundedFile(path, connections.MaxInputBytes)
	if err != nil {
		t.Fatalf("large arguments file rejected: %v", err)
	}
	if len(raw) != len(payload) {
		t.Fatalf("read %d bytes, want %d", len(raw), len(payload))
	}
}
