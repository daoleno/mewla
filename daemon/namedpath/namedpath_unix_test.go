//go:build !windows

package namedpath

import (
	"path/filepath"
	"testing"
)

// Off Windows a backslash is part of a file name, not a separator: `~\x` is a name in the cwd, never the home.
func TestBackslashIsANameOffWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	writeFiles(t, home, "notes.html", "site/index.html")
	writeFiles(t, cwd, `~\notes.html`)
	if got := opens(cwd, `~\notes.html`); got != canonical(t, filepath.Join(cwd, `~\notes.html`)) {
		t.Fatalf(`~\notes.html opens %q`, got)
	}
	if got := opens(home, `site\`); got != "" {
		t.Fatalf(`site\ opens %q`, got)
	}
}
