//go:build !windows

package agentuse

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// nativeHostLauncherExt names the launcher Chrome runs: a shell script.
const nativeHostLauncherExt = ".sh"

// nativeHostLauncher runs `mewla agent-use native-host`. Chrome's own
// argument, the extension's origin, is not passed on.
func nativeHostLauncher(mewla, stateDir, flag string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\nexec %s agent-use native-host --state-dir %s%s\n", shellQuote(mewla), shellQuote(stateDir), flag))
}

// registerNativeHost puts the host manifest in the profile's
// NativeMessagingHosts folder, where Chrome looks for the user's own hosts.
func (s *Service) registerNativeHost(userDataDir string, _ bool, manifest []byte) error {
	dir := filepath.Join(userDataDir, "NativeMessagingHosts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(dir, NativeHostName+".json"), manifest)
}

// openShim writes the program the browser tools start in place of Chrome.
// Playwright's tools run `<executable> [--profile-directory=…] <url>` to make
// Chrome open the extension's connect page; the shim reads the relay address
// from that URL and has Mewla's extension connect to it instead.
func (s *Service) openShim(mewla string) (string, []string, error) {
	path := filepath.Join(s.dir, "chrome-for-tools.sh")
	script := fmt.Sprintf("#!/bin/sh\nexec %s agent-use open --state-dir %s -- \"$@\"\n", shellQuote(mewla), shellQuote(filepath.Dir(s.dir)))
	return path, nil, atomicfile.Write(path, []byte(script), 0o700)
}

// startChromeProcess starts Chrome for the extension or a fix.
func startChromeProcess(cmd *exec.Cmd) error { return cmd.Start() }
