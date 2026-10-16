package agentuse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// computer is a fake machine: a Chrome install, a user data dir and a Node.
type computer struct {
	t       *testing.T
	root    string
	chrome  chromeInstall
	node    string
	npm     string
	hasNode bool
	// desktop is the session found for Chrome; noDesktop means none.
	desktop   []string
	noDesktop bool
}

func newFakeComputer(t *testing.T) *computer {
	t.Helper()
	root := t.TempDir()
	c := &computer{t: t, root: root}
	c.chrome = chromeInstall{Executable: filepath.Join(root, "chrome"), UserDataDir: filepath.Join(root, "chrome-data")}
	// The fake Chrome records how it was opened.
	c.script(c.chrome.Executable, `printf '%s\n' "$@" > "`+filepath.Join(root, "chrome-opened")+`"`)
	if err := os.MkdirAll(filepath.Join(c.chrome.UserDataDir, "Default"), 0o700); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *computer) script(path, body string) {
	c.t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		c.t.Fatal(err)
	}
}

// withNode adds a Node whose npm "installs" the MCP server by creating its
// entry point, the way npm ci would.
func (c *computer) withNode() {
	bin := filepath.Join(c.root, "node-bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		c.t.Fatal(err)
	}
	c.node, c.npm, c.hasNode = filepath.Join(bin, "node"), filepath.Join(bin, "npm"), true
	c.script(c.node, `echo v22.1.0`)
	c.script(c.npm, `test -f package-lock.json || exit 3; mkdir -p node_modules/@playwright/mcp && echo '// cli' > node_modules/@playwright/mcp/cli.js`)
}

func (c *computer) installExtension(profile string) {
	c.t.Helper()
	if err := os.MkdirAll(filepath.Join(c.chrome.UserDataDir, profile, "Extensions", ExtensionID, "0.4.0_0"), 0o700); err != nil {
		c.t.Fatal(err)
	}
}

func (c *computer) service(prepare bool) *Service {
	c.t.Helper()
	s, err := New(filepath.Join(c.root, "state"), prepare)
	if err != nil {
		c.t.Fatal(err)
	}
	s.lookChrome = func(string) (chromeInstall, bool) { return c.chrome, true }
	s.lookNode = func(string) (string, string, bool) { return c.node, c.npm, c.hasNode }
	s.lookDesktop = func() ([]string, bool) { return c.desktop, !c.noDesktop }
	return s
}

func TestBrowserStatusWalksTheUserThroughWhatIsMissing(t *testing.T) {
	c := newFakeComputer(t)
	s := c.service(true)

	status := s.Refresh()
	if !status.Enabled || status.Browser.State != BrowserNoExtension || status.Browser.Fix != FixInstallExtension {
		t.Fatalf("no extension: %+v", status)
	}
	if _, err := s.Fix(FixInstallExtension); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(c.root, "chrome-opened"), storeURL)

	c.installExtension("Default")
	if status := s.Refresh(); status.Browser.State != BrowserNoKey || status.Browser.Fix != FixOpenExtension {
		t.Fatalf("no key: %+v", status)
	}
	if _, err := s.Fix(FixOpenExtension); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(c.root, "chrome-opened"), "--profile-directory=Default\n"+extensionPageURL)

	writeSQLiteStorage(t, filepath.Join(c.chrome.UserDataDir, "Default"), append([]byte{1}, validKey...))
	if status := s.Refresh(); status.Browser.State != BrowserNoNode || status.Browser.Fix != "" {
		t.Fatalf("no node: %+v", status)
	}

	c.withNode()
	if status := s.Refresh(); status.Browser.State != BrowserPreparing {
		t.Fatalf("preparing: %+v", status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for s.Refresh().Browser.State != BrowserReady {
		if time.Now().After(deadline) {
			t.Fatalf("never ready: %+v", s.Refresh())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.BrowserReady() {
		t.Fatal("BrowserReady disagrees with Status")
	}
}

func TestMCPServerPassesTheKeyOnlyInTheEnvironment(t *testing.T) {
	c := newFakeComputer(t)
	c.installExtension("Profile 2")
	writeSQLiteStorage(t, filepath.Join(c.chrome.UserDataDir, "Profile 2"), append([]byte{1}, validKey...))
	c.withNode()
	s := c.service(true)
	waitReady(t, s)

	argv, env, err := s.MCPServer()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{c.node, "@playwright/mcp/cli.js", "--extension", "--executable-path " + c.chrome.Executable, "--profile-dir-name Profile 2", "--snapshot-mode none", "--codegen none", "--output-dir " + filepath.Join(c.root, "state", "agent-use", "browser-files"), "--output-max-size ", "--file-paths absolute"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv %q lacks %q", joined, want)
		}
	}
	if strings.Contains(joined, validKey) {
		t.Fatal("the key is on the command line")
	}
	if info, err := os.Stat(filepath.Join(c.root, "state", "agent-use", "browser-files")); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("browser files dir: %v %v", info, err)
	}
	keys := 0
	for _, entry := range env {
		if entry == "PLAYWRIGHT_MCP_EXTENSION_TOKEN="+validKey {
			keys++
		}
		if strings.HasPrefix(entry, "PATH=") && !strings.HasPrefix(entry, "PATH="+filepath.Dir(c.node)) {
			t.Fatalf("node's directory is not first on PATH: %s", entry)
		}
	}
	if keys != 1 {
		t.Fatalf("key appears %d times in the environment", keys)
	}
}

func TestTheSwitchIsSharedAndOffMeansNoBrowser(t *testing.T) {
	c := newFakeComputer(t)
	c.installExtension("Default")
	writeSQLiteStorage(t, filepath.Join(c.chrome.UserDataDir, "Default"), append([]byte{1}, validKey...))
	c.withNode()
	daemon := c.service(true)
	waitReady(t, daemon)

	// The mewla command turns it off; the daemon sees it on its next look.
	if _, err := c.service(false).SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if status := daemon.Refresh(); status.Enabled || status.Browser.State != BrowserOff {
		t.Fatalf("off: %+v", status)
	}
	if _, _, err := daemon.MCPServer(); err == nil {
		t.Fatal("browser tools start while agents are turned off")
	}
	raw, _ := os.ReadFile(filepath.Join(c.root, "state", "agent-use", "settings.json"))
	var stored map[string]any
	_ = json.Unmarshal(raw, &stored)
	if stored["off"] != true {
		t.Fatalf("settings: %s", raw)
	}
	if _, err := daemon.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	waitReady(t, daemon)
}

func TestPastedKeyWinsAndIsValidated(t *testing.T) {
	c := newFakeComputer(t)
	c.installExtension("Default")
	c.withNode()
	s := c.service(true)
	if status := s.Refresh(); status.Browser.State != BrowserNoKey {
		t.Fatalf("no key: %+v", status)
	}
	if _, err := s.SetKey("not a key; rm -rf"); err == nil {
		t.Fatal("accepted a malformed key")
	}
	if _, err := s.SetKey("PLAYWRIGHT_MCP_EXTENSION_TOKEN=" + validKey); err != nil {
		t.Fatal(err)
	}
	waitReady(t, s)
	if !s.Refresh().Browser.KeyPasted {
		t.Fatal("pasted key not reported")
	}
	info, err := os.Stat(filepath.Join(c.root, "state", "agent-use", "settings.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file mode: %v %v", info.Mode(), err)
	}
	if _, err := s.SetKey(""); err != nil {
		t.Fatal(err)
	}
	if status := s.Refresh(); status.Browser.State != BrowserNoKey || status.Browser.KeyPasted {
		t.Fatalf("cleared key: %+v", status)
	}
}

func TestAFailedDownloadSaysWhyAndCanBeRetried(t *testing.T) {
	c := newFakeComputer(t)
	c.installExtension("Default")
	writeSQLiteStorage(t, filepath.Join(c.chrome.UserDataDir, "Default"), append([]byte{1}, validKey...))
	c.withNode()
	c.script(c.npm, `echo "npm error network ENOTFOUND registry.npmjs.org" >&2; exit 1`)
	s := c.service(true)
	s.Refresh()
	deadline := time.Now().Add(10 * time.Second)
	for s.Refresh().Browser.State != BrowserFailed {
		if time.Now().After(deadline) {
			t.Fatalf("never failed: %+v", s.Refresh())
		}
		time.Sleep(20 * time.Millisecond)
	}
	status := s.Refresh()
	if status.Browser.Fix != FixRetry || !strings.Contains(status.Browser.Summary, "ENOTFOUND") {
		t.Fatalf("failed: %+v", status)
	}
	c.withNode()
	if _, err := s.Fix(FixRetry); err != nil {
		t.Fatal(err)
	}
	waitReady(t, s)
}

func TestProfileChoiceMatchesPlaywright(t *testing.T) {
	dir := t.TempDir()
	for _, profile := range []string{"Default", "Profile 1", "Profile 3", "System Profile"} {
		if err := os.MkdirAll(filepath.Join(dir, profile), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := extensionProfile(dir); ok {
		t.Fatal("found an extension that is not installed")
	}
	// Installed through Preferences only (no Extensions directory yet).
	prefs := `{"extensions":{"settings":{"` + ExtensionID + `":{"state":1}}}}`
	for _, profile := range []string{"Profile 1", "Profile 3"} {
		if err := os.WriteFile(filepath.Join(dir, profile, "Preferences"), []byte(prefs), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if profile, _ := extensionProfile(dir); profile != "Profile 1" {
		t.Fatalf("lowest profile first: %q", profile)
	}
	if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte(`{"profile":{"last_used":"Profile 3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if profile, _ := extensionProfile(dir); profile != "Profile 3" {
		t.Fatalf("last used first: %q", profile)
	}
}

func TestNodeVersionsSortNewestFirst(t *testing.T) {
	if nodeVersionFromPath("/h/.nvm/versions/node/v22.11.0/bin/node") <= nodeVersionFromPath("/h/.nvm/versions/node/v20.18.3/bin/node") {
		t.Fatal("v22 should sort before v20")
	}
	if nodeVersionFromPath("/h/.local/share/mise/installs/node/24.18.0/bin/node") <= nodeVersionFromPath("/h/.local/share/mise/installs/node/9.11.2/bin/node") {
		t.Fatal("24 should sort before 9")
	}
}

func waitReady(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for s.Refresh().Browser.State != BrowserReady {
		if time.Now().After(deadline) {
			t.Fatalf("never ready: %+v", s.Refresh())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, _ := os.ReadFile(path)
		if strings.TrimSpace(string(raw)) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q, want %q", path, raw, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNoDesktopIsNotReady(t *testing.T) {
	c := newFakeComputer(t)
	c.installExtension("Default")
	writeSQLiteStorage(t, filepath.Join(c.chrome.UserDataDir, "Default"), append([]byte{1}, validKey...))
	c.withNode()
	c.noDesktop = true
	s := c.service(true)
	if status := s.Refresh(); status.Browser.State != BrowserNoDesktop {
		t.Fatalf("no desktop: %+v", status)
	}
	c.noDesktop = false
	c.desktop = []string{"WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=/run/user/1000"}
	waitReady(t, s)
	_, env, err := s.MCPServer()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range env {
		if entry == "WAYLAND_DISPLAY=wayland-0" || entry == "XDG_RUNTIME_DIR=/run/user/1000" {
			found++
		}
		if strings.HasPrefix(entry, "XDG_RUNTIME_DIR=") && entry != "XDG_RUNTIME_DIR=/run/user/1000" {
			t.Fatalf("the desktop's variable is duplicated: %s", entry)
		}
	}
	if found != 2 {
		t.Fatalf("desktop session not passed to Chrome: %v", env)
	}
}

// A Mac left running: the extension and its key arrive while nothing asks
// for the status, and the daemon still fetches the browser tools.
func TestTheDaemonNoticesChromeGettingReadyOnItsOwn(t *testing.T) {
	c := newFakeComputer(t)
	c.withNode()
	s := c.service(true)
	s.recheck = 10 * time.Millisecond
	if status := s.Refresh(); status.Browser.State != BrowserNoExtension {
		t.Fatalf("no extension: %+v", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	c.installExtension("Default")
	writeSQLiteStorage(t, filepath.Join(c.chrome.UserDataDir, "Default"), append([]byte{1}, validKey...))
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		state := s.cached.Browser.State
		s.mu.Unlock()
		if state == BrowserReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never ready on its own: %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
