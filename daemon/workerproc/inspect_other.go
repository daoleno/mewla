//go:build !linux && !darwin

package workerproc

// Environ, Args, Descendants and OpenFiles have no implementation on this
// platform.
func Environ(int) []string   { return nil }
func Args(int) []string      { return nil }
func Descendants(int) []int  { return nil }
func OpenFiles(int) []string { return nil }
