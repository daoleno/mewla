//go:build !linux

package gc

// ReadUsage: only Linux can tell which paths processes hold.
func ReadUsage() (Usage, bool) { return Usage{}, false }
