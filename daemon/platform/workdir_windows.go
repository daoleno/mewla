//go:build windows

package platform

import (
	"fmt"
	"unicode/utf16"
)

// maxWorkDir is the longest folder Windows starts a program in: a working
// directory must fit MAX_PATH (260) with its trailing backslash and the
// terminating NUL, whatever the long-path setting says.
const maxWorkDir = 258

// CheckWorkDir says, in words, why a program cannot be started in dir.
func CheckWorkDir(dir string) error {
	if n := len(utf16.Encode([]rune(dir))); n > maxWorkDir {
		return fmt.Errorf("Windows cannot start a program in %s: its path is %d characters, and Windows allows %d for a program's folder. Choose a folder with a shorter path", dir, n, maxWorkDir)
	}
	return nil
}
