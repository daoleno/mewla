package connections

import (
	"os"
	"path/filepath"
)

// gws loads a .env file from its working directory or a parent. It runs in an
// empty directory owned by this daemon, under Mewla's state, never a shared
// location such as /tmp.
func cloudWorkDir(binary string) string {
	if filepath.Base(binary) != "gws" {
		return ""
	}
	root := os.Getenv("MEWLA_STATE_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return gwsFallbackWorkDir()
		}
		root = filepath.Join(home, ".mewla")
	}
	dir := filepath.Join(root, "cli", "gws", "run")
	if os.MkdirAll(dir, 0700) != nil {
		return gwsFallbackWorkDir()
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || !ownedByUs(info) {
		return gwsFallbackWorkDir()
	}
	return dir
}

// A unique directory we just created is ours by construction.
func gwsFallbackWorkDir() string {
	dir, err := os.MkdirTemp("", "mewla-gws-")
	if err != nil {
		return ""
	}
	return dir
}
