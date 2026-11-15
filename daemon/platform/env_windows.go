//go:build windows

package platform

import "golang.org/x/sys/windows"

// userEnvironment is the environment Windows gives a program started now
// (CreateEnvironmentBlock without this process's own): the system and user
// variables as the registry holds them at this moment, expanded.
func userEnvironment() ([]string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_IMPERSONATE, &token); err != nil {
		return nil, err
	}
	defer token.Close()
	return token.Environ(false)
}
