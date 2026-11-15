package platform

import "io/fs"

// runnableFile reports whether a file found on PATH can be started. Besides
// regular files that includes what Go reports as irregular: on Windows, a
// Microsoft Store app's execution alias (PowerShell 7 from the Store is
// WindowsApps\pwsh.exe), a zero-byte reparse point CreateProcess starts.
// Directories, pipes and devices are not programs.
func runnableFile(mode fs.FileMode) bool {
	return mode.IsRegular() || mode.Type() == fs.ModeIrregular
}
