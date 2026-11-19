package platform

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// longDir is a real directory whose path is too long for a unix socket.
func longDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), strings.Repeat("a-very-long-state-directory-name/", 4))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSocketPathKeepsAPathThatFits(t *testing.T) {
	path := filepath.Join(os.TempDir(), "m", "control.sock")
	if got := SocketPath(path); got != path {
		t.Fatalf("SocketPath(%q) = %q", path, got)
	}
}

func TestSocketPathShortensALongPathToOneThatBinds(t *testing.T) {
	dir := longDir(t)
	long := filepath.Join(dir, "codex-ctl-0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0.sock")
	short := SocketPath(long)
	if len(short) >= socketPathLimit() || filepath.Dir(short) != ShortSocketDir() {
		t.Fatalf("SocketPath(%d bytes) = %q", len(long), short)
	}
	if !strings.HasPrefix(filepath.Base(short), "codex-ctl-0b1c2d") {
		t.Fatalf("short name %q keeps no hint of %q", short, filepath.Base(long))
	}
	if again := SocketPath(long); again != short {
		t.Fatalf("SocketPath is not stable: %q then %q", short, again)
	}
	// Relative and uncleaned spellings of the same path agree.
	t.Chdir(dir)
	if got := SocketPath("./x/../codex-ctl-0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0.sock"); got != short {
		t.Fatalf("relative spelling = %q, want %q", got, short)
	}
	if other := SocketPath(filepath.Join(dir, "codex-ctl-other.sock")); other == short {
		t.Fatal("two paths share a short socket")
	}
	if err := EnsureSocketDir(short); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", short)
	if err != nil {
		t.Fatalf("listen on %q: %v", short, err)
	}
	defer listener.Close()
	conn, err := net.Dial("unix", SocketPath(long))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

func TestEnsureSocketDirLeavesAnOrdinaryDirectoryAlone(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := EnsureSocketDir(filepath.Join(missing, "a.sock")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("EnsureSocketDir made %s", missing)
	}
}
