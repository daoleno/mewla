package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultWorkDir is the folder to use for cwd when the person chose none: cwd
// itself, except inside the Windows folder, where an Administrator PowerShell
// starts (C:\WINDOWS\system32). Mewla installed from there would otherwise
// start every Session in system32 and Claude would ask to trust it; the
// person's home folder is used instead.
func DefaultWorkDir(cwd string) string {
	if runtime.GOOS != "windows" {
		return cwd
	}
	home, _ := os.UserHomeDir()
	return defaultWorkDir(cwd, home, os.Getenv("SystemRoot"))
}

func defaultWorkDir(cwd, home, systemRoot string) string {
	if home == "" || systemRoot == "" || cwd == "" {
		return cwd
	}
	clean := strings.ToLower(filepath.Clean(cwd))
	root := strings.ToLower(filepath.Clean(systemRoot))
	if clean == root || strings.HasPrefix(clean, strings.TrimRight(root, `\/`)+string(filepath.Separator)) {
		return home
	}
	return cwd
}
