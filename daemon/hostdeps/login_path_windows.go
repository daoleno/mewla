//go:build windows

package hostdeps

// Windows reads the person's environment from the registry
// (platform.RefreshEnvironment); there is no login shell to ask.
func LoginShellPathDirs() []string { return nil }

func nodeInstallDirs(string) []string { return nil }
