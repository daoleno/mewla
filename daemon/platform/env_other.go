//go:build !windows

package platform

// userEnvironment has nothing to add on Unix: each Session's login shell
// reads the person's rc files itself.
func userEnvironment() ([]string, error) { return nil, nil }
