// Package statedir owns the default Mewla state root (~/.mewla).
package statedir

import "path/filepath"

// DirName is the state root under the user's home directory.
const DirName = ".mewla"

// Default returns the state root for home.
func Default(home string) string {
	return filepath.Join(home, DirName)
}
