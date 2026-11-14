package ghosttyvt

import "testing"

func TestLocalDirDecodesOSC7Reports(t *testing.T) {
	for _, tc := range []struct {
		name, report, host string
		windows            bool
		want               string
	}{
		{"macOS zsh", "file://Mac.lan/Users/daoleno/.mewla/brain/workspace", "Mac.lan", false, "/Users/daoleno/.mewla/brain/workspace"},
		{"host case and .local", "file://mac.local/Users/x", "Mac.lan", false, "/Users/x"},
		{"space", "file://Mac/Users/x/a%20b", "Mac", false, "/Users/x/a b"},
		{"encoded unicode", "file://Mac/Users/x/%E9%A1%B9%E7%9B%AE", "Mac", false, "/Users/x/项目"},
		{"raw unicode", "file://Mac/Users/x/项目 b", "Mac", false, "/Users/x/项目 b"},
		{"empty host", "file:///home/x", "box", false, "/home/x"},
		{"localhost", "file://localhost/home/x", "box", false, "/home/x"},
		{"trailing slash", "file:///home/x/", "box", false, "/home/x"},
		{"already a path", "/Users/x/a b", "Mac", false, "/Users/x/a b"},
		{"foreign host", "file://elsewhere.example/home/x", "Mac.lan", false, ""},
		{"unknown own host", "file://Mac/Users/x", "", false, ""},
		{"non-file scheme", "kitty-shell-cwd://Mac/Users/x", "Mac", false, ""},
		{"https", "https://Mac/Users/x", "Mac", false, ""},
		{"relative", "file:x/y", "Mac", false, ""},
		{"not a path", "workspace", "Mac", false, ""},
		{"empty", "", "Mac", false, ""},
		{"windows drive", "file://PC/C:/Users/x%20y", "PC", true, `C:\Users\x y`},
		{"windows path", `C:\Users\x`, "PC", true, `C:\Users\x`},
		{"windows no drive", "file://PC/Users/x", "PC", true, ""},
	} {
		if got := localDir(tc.report, tc.host, tc.windows); got != tc.want {
			t.Errorf("%s: localDir(%q) = %q, want %q", tc.name, tc.report, got, tc.want)
		}
	}
}
