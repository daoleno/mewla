package platform

import (
	"path/filepath"
	"testing"
)

func TestDefaultWorkDirLeavesTheWindowsFolder(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + "WINDOWS"
	home := sep + "Users" + sep + "me"
	for _, tc := range []struct{ cwd, want string }{
		{root + sep + "system32", home},
		{sep + "windows" + sep + "System32", home},
		{root, home},
		{home + sep + "project", home + sep + "project"},
		{sep + "WindowsApps", sep + "WindowsApps"},
		{"", ""},
	} {
		if got := defaultWorkDir(tc.cwd, home, root); got != tc.want {
			t.Errorf("defaultWorkDir(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}
