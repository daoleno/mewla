package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultFollowsAnExplicitRootOnly(t *testing.T) {
	t.Cleanup(func() { SetRoot("") })
	home := filepath.Join(string(filepath.Separator), "home", "u")
	if got := Default(home); got != filepath.Join(home, ".mewla") {
		t.Fatalf("default = %q", got)
	}
	root := filepath.Join(string(filepath.Separator), "tmp", "onb")
	SetRoot(root)
	if got := Default(home); got != Canonical(root) {
		t.Fatalf("relocated = %q, want %q", got, root)
	}
	SetRoot("")
	if got := Default(home); got != filepath.Join(home, ".mewla") {
		t.Fatalf("restored = %q", got)
	}
}

func TestInitFromEnvIgnoresARelativePath(t *testing.T) {
	t.Cleanup(func() { SetRoot("") })
	t.Setenv(RootEnv, "relative/dir")
	InitFromEnv()
	if got := Default("/h"); got != filepath.Join("/h", ".mewla") {
		t.Fatalf("a relative root moved state: %q", got)
	}
	abs := filepath.Join(string(filepath.Separator), "tmp", "onb")
	t.Setenv(RootEnv, abs)
	InitFromEnv()
	if got := Default("/h"); got != Canonical(abs) {
		t.Fatalf("absolute root = %q, want %q", got, abs)
	}
}

// macOS's /tmp is a symlink; a root reached through one must resolve to the real
// path, whether or not its last parts exist yet.
func TestSetRootResolvesASymlinkedPath(t *testing.T) {
	t.Cleanup(func() { SetRoot("") })
	real := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(real); err == nil {
		real = resolved
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	SetRoot(filepath.Join(link, "state", "root"))
	if want := filepath.Join(real, "state", "root"); Default("/h") != want {
		t.Fatalf("root = %q, want %q", Default("/h"), want)
	}
	if got := Canonical(filepath.Join(link, "x")); got != filepath.Join(real, "x") {
		t.Fatalf("canonical = %q", got)
	}
	if got := Canonical("relative-and-missing/dir"); got == "" {
		t.Fatal("empty result")
	}
}
