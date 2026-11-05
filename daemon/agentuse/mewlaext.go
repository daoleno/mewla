package agentuse

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// Mewla for Chrome is Mewla's own extension, derived from Playwright's. It
// needs no key: Chrome starts Mewla's native messaging host only for this
// extension's ID, and the host joins it to the daemon (see bridge.go). It
// opens the agent's tabs in a window of their own, which the person's tabs
// never share and which can sit on the display Mewla keeps for agents.

//go:embed extension/manifest.json extension/background.js extension/relay.js extension/status.html extension/status.js extension/LICENSE-NOTICE.md extension/icons/*.png
var extensionFiles embed.FS

// MewlaExtensionID is the ID the key in the extension's own manifest fixes:
// the folder Mewla writes for loading unpacked has it. The Chrome Web Store
// assigns its own ID on the first upload (its manifest carries no key), which
// is MewlaStoreExtensionID once the item exists.
const MewlaExtensionID = "keclgeaemhlgnnhocopjklenhgfimmjh"

// MewlaStoreExtensionID is the Chrome Web Store item's ID; empty until the
// first upload has assigned it (see the submit steps).
const MewlaStoreExtensionID = ""

// mewlaExtensionIDs are the IDs the extension can have.
func mewlaExtensionIDs() []string {
	ids := []string{MewlaExtensionID}
	if MewlaStoreExtensionID != "" {
		ids = append([]string{MewlaStoreExtensionID}, ids...)
	}
	return ids
}

// installedMewlaExtension is the Chrome profile that has the extension, and
// the IDs it has it under. Only an ID found installed is allowed to talk to
// the native host, so an extension nobody added can't claim the other one's.
func installedMewlaExtension(userDataDir string) (profile string, ids []string, ok bool) {
	for _, id := range mewlaExtensionIDs() {
		p, found := profileWithExtension(userDataDir, id)
		if !found {
			continue
		}
		if profile == "" {
			profile = p
		}
		if p == profile {
			ids = append(ids, id)
		}
	}
	return profile, ids, profile != ""
}

// NativeHostName is the name the extension asks Chrome for.
const NativeHostName = "com.mewla.browser"

// MewlaStoreURL is where the person adds the extension. It stays empty until
// the extension is published; until then it is added from the folder
// ExtensionDir writes.
var MewlaStoreURL = ""

// ExtensionDir writes the extension's files and returns the folder, which
// Chrome can load unpacked.
func (s *Service) ExtensionDir() (string, error) {
	dir := filepath.Join(s.dir, "chrome-extension")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	err := fs.WalkDir(extensionFiles, "extension", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := extensionFiles.ReadFile(path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "extension/")
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			return err
		}
		return atomicfile.Write(filepath.Join(dir, rel), raw, 0o644)
	})
	return dir, err
}

// hostManifest is Chrome's native messaging host manifest.
type hostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// InstallNativeHost writes the host manifest where Chrome looks for the
// user's own (no administrator rights), and the launcher it points to. Chrome
// runs the launcher with the extension's origin as its argument; the launcher
// runs `mewla agent-use native-host`. It is written again whenever the Mewla
// binary moves.
func (s *Service) InstallNativeHost(userDataDir, mewla string, extensionIDs []string) error {
	launcher := filepath.Join(s.dir, "native-host.sh")
	script := fmt.Sprintf("#!/bin/sh\nexec %s agent-use native-host --state-dir %s\n", shellQuote(mewla), shellQuote(filepath.Dir(s.dir)))
	if err := atomicfile.Write(launcher, []byte(script), 0o700); err != nil {
		return err
	}
	manifest := hostManifest{
		Name:        NativeHostName,
		Description: "Mewla: lets the agents Mewla starts use this Chrome in a window of their own",
		Path:        launcher,
		Type:        "stdio",
	}
	for _, id := range extensionIDs {
		manifest.AllowedOrigins = append(manifest.AllowedOrigins, "chrome-extension://"+id+"/")
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(userDataDir, "NativeMessagingHosts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, NativeHostName+".json")
	if old, err := os.ReadFile(path); err == nil && string(old) == string(raw)+"\n" {
		return nil
	}
	return atomicfile.Write(path, append(raw, '\n'), 0o644)
}

// writeOpenShim writes the program the browser tools start in place of
// Chrome. Playwright's tools run `<executable> [--profile-directory=…] <url>`
// to make Chrome open the extension's connect page; the shim reads the relay
// address from that URL and has Mewla's extension connect to it instead.
func (s *Service) writeOpenShim(mewla string) (string, error) {
	path := filepath.Join(s.dir, "chrome-for-tools.sh")
	script := fmt.Sprintf("#!/bin/sh\nexec %s agent-use open --state-dir %s -- \"$@\"\n", shellQuote(mewla), shellQuote(filepath.Dir(s.dir)))
	return path, atomicfile.Write(path, []byte(script), 0o700)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// toolsRequest is what the shim's arguments say: the relay to connect to,
// the client's name, and the profile Chrome would have opened.
type toolsRequest struct {
	RelayURL string
	Client   string
	Profile  string
}

func parseToolsArgs(args []string) (toolsRequest, error) {
	var req toolsRequest
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--profile-directory="):
			req.Profile = strings.TrimPrefix(a, "--profile-directory=")
		case strings.HasPrefix(a, "chrome-extension://"):
			u, err := url.Parse(a)
			if err != nil {
				return req, err
			}
			req.RelayURL = u.Query().Get("mcpRelayUrl")
			var client struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal([]byte(u.Query().Get("client")), &client)
			req.Client = client.Name
		}
	}
	if req.RelayURL == "" {
		return req, errors.New("no relay address in the browser tools' request")
	}
	return req, nil
}

// OpenForTools answers the browser tools' request to open Chrome: the Mewla
// extension connects to their relay in a window of its own. When Chrome isn't
// running it is started without a window, only to run the extension.
func OpenForTools(stateDir string, args []string, startChrome func(profile string) error) error {
	req, err := parseToolsArgs(args)
	if err != nil {
		return err
	}
	dir := filepath.Join(stateDir, "agent-use")
	started := false
	deadline := time.Now().Add(25 * time.Second)
	for {
		err := ConnectViaBridge(dir, req.RelayURL, req.Client)
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "isn't connected") {
			return err
		}
		if !started {
			started = true
			if startChrome != nil {
				if serr := startChrome(req.Profile); serr != nil {
					return fmt.Errorf("couldn't start Chrome: %w", serr)
				}
			}
		}
		if time.Now().After(deadline) {
			return errors.New("Chrome started but the Mewla extension did not connect: is it added to this profile, and is Chrome allowed to run it?")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// StartChromeForExtension starts Chrome in the background with no window, as
// Chrome does to run an extension alone. On a Mac it is opened with -g, so it
// doesn't come to the front of what the person is using.
func StartChromeForExtension(chrome chromeInstall, profile string) error {
	args := []string{"--no-startup-window", "--no-first-run"}
	if profile != "" {
		args = append(args, "--profile-directory="+profile)
	}
	cmd := exec.Command(chrome.Executable, args...)
	if runtime.GOOS == "darwin" {
		if app := macAppOf(chrome.Executable); app != "" {
			cmd = exec.Command("/usr/bin/open", append([]string{"-g", "-j", "-a", app, "--args"}, args...)...)
		}
	}
	if runtime.GOOS == "linux" {
		cmd.Env = os.Environ()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// macAppOf is the .app bundle holding an executable inside it, or "".
func macAppOf(executable string) string {
	if i := strings.Index(executable, ".app/Contents/MacOS/"); i >= 0 {
		return executable[:i+len(".app")]
	}
	return ""
}

// FindChrome is the user's Google Chrome, for the commands that start it.
func FindChrome() (chromeInstall, bool) {
	home, _ := os.UserHomeDir()
	return findChrome(home)
}
