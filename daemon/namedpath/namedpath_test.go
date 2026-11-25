package namedpath

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFiles writes each relative path (slash-separated) under root.
func writeFiles(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<h1>x</h1>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// opens: the regular file a reference names (a folder's index.html for a folder named with its separator), or "".
func opens(cwd, reference string) string {
	_, path, err := Resolve(cwd, reference)
	if err != nil {
		return ""
	}
	path = FolderPage(path, reference)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// The forms every OS shares: relative, ./, ~/, absolute, and a folder named with its slash opening its index.html.
func TestNamedPathsOpenTheFileOrTheFoldersPage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	writeFiles(t, cwd, "reports/q3.html", "site/index.html", "输出/黄金策略图解.html", "empty/notes.txt")
	writeFiles(t, home, "mewla-path-test/home.html", "pages/index.html")
	for reference, want := range map[string]string{
		"reports/q3.html":                       filepath.Join(cwd, "reports", "q3.html"),
		"./reports/q3.html":                     filepath.Join(cwd, "reports", "q3.html"),
		"输出/黄金策略图解.html":                        filepath.Join(cwd, "输出", "黄金策略图解.html"),
		filepath.Join(cwd, "输出", "黄金策略图解.html"): filepath.Join(cwd, "输出", "黄金策略图解.html"),
		"site/": filepath.Join(cwd, "site", "index.html"),
		filepath.ToSlash(filepath.Join(cwd, "site")) + "/": filepath.Join(cwd, "site", "index.html"),
		"~/mewla-path-test/home.html":                      filepath.Join(home, "mewla-path-test", "home.html"),
		"~/pages/":                                         filepath.Join(home, "pages", "index.html"),
	} {
		if got := opens(cwd, reference); got != canonical(t, want) {
			t.Errorf("%q opens %q, want %q", reference, got, want)
		}
	}
	for _, reference := range []string{"site", "empty/", "missing/", "reports/missing.html", ""} {
		if got := opens(cwd, reference); got != "" {
			t.Errorf("%q opens %q, want nothing", reference, got)
		}
	}
}
