//go:build windows

package agentuse

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/platform"
)

// nativeHostLauncherExt names the launcher Chrome runs: Chrome on Windows
// starts a native host through cmd.exe, so a batch file can be one.
const nativeHostLauncherExt = ".bat"

// nativeHostLauncher runs `mewla agent-use native-host`. Echo is off: an
// echoed line on stdout would break the messages' framing. Chrome's own
// arguments (the origin and --parent-window) are not passed on, and a % in a
// path is doubled, as a batch file reads it.
func nativeHostLauncher(mewla, stateDir, flag string) []byte {
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, "%", "%%") + `"` }
	return []byte("@echo off\r\n" + quote(mewla) + " agent-use native-host --state-dir " + quote(stateDir) + flag + "\r\n")
}

// nativeHostsKey is where Chrome on Windows finds a user's own native hosts:
// it reads no folder in the profile, only this key's default value, the
// manifest's path. HKEY_CURRENT_USER needs no administrator rights.
var nativeHostsKey = `Software\Google\Chrome\NativeMessagingHosts\` + NativeHostName

// registerNativeHost writes the manifest beside the launcher, in Mewla's
// state, and points Chrome's key at it.
func (s *Service) registerNativeHost(_ string, agent bool, manifest []byte) error {
	if agent {
		return errors.New("the agent browser runs only on Linux")
	}
	path := filepath.Join(s.dir, NativeHostName+".json")
	if err := writeIfChanged(path, manifest); err != nil {
		return err
	}
	return platform.SetUserRegistryDefault(nativeHostsKey, path)
}

// openShim is Mewla's own executable, with OpenShimEnv naming the state
// directory: Playwright's tools start the browser without a shell, which on
// Windows runs no script, only a program. Mewla started with OpenShimEnv set
// runs `agent-use open` with the arguments meant for Chrome.
func (s *Service) openShim(mewla string) (string, []string, error) {
	return mewla, []string{OpenShimEnv + "=" + filepath.Dir(s.dir)}, nil
}

// startChromeProcess starts Chrome outside the caller's job: a Worker's job,
// or the Task Scheduler's for the daemon, ends every process in it, and the
// person's Chrome must not end with an agent or a restart of Mewla.
func startChromeProcess(cmd *exec.Cmd) error { return platform.StartDetached(cmd) }
