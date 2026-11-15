//go:build !windows

package platform

// CheckWorkDir says why a program cannot be started in dir; only Windows
// limits the folder's length (see workdir_windows.go).
func CheckWorkDir(string) error { return nil }
