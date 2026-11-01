package statedir

import (
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
	if got := Default(home); got != root {
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
	if got := Default("/h"); got != abs {
		t.Fatalf("absolute root = %q, want %q", got, abs)
	}
}
