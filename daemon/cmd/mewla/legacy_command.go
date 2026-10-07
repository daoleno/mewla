package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ensureMewlaCommand gives a pre-rename install that self-updated in place
// (a real `zen` executable) a `mewla -> zen` sibling, so prompts and docs that
// call `mewla` resolve from the same directory. Through the installer's
// `zen -> mewla` alias, os.Executable is `mewla` on Linux and may stay `zen` on
// darwin, where the existing `mewla` sibling stops it. It never replaces an
// existing name and ignores directories it cannot write.
func ensureMewlaCommand(executable string) {
	if filepath.Base(executable) != "zen" {
		return
	}
	sibling := filepath.Join(filepath.Dir(executable), "mewla")
	if _, err := os.Lstat(sibling); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	_ = os.Symlink("zen", sibling)
}
