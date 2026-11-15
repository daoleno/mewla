package platform

import (
	"io/fs"
	"testing"
)

func TestRunnableFileTakesAStoreAppAliasButNotADirectory(t *testing.T) {
	for _, c := range []struct {
		mode fs.FileMode
		want bool
	}{
		{0o644, true},
		{fs.ModeIrregular | 0o666, true}, // WindowsApps\pwsh.exe as os.Stat sees it
		{fs.ModeDir | 0o755, false},
		{fs.ModeNamedPipe, false},
		{fs.ModeDevice, false},
	} {
		if got := runnableFile(c.mode); got != c.want {
			t.Errorf("runnableFile(%v) = %v, want %v", c.mode, got, c.want)
		}
	}
}
