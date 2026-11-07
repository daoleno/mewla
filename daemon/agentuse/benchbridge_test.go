package agentuse

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The real browser chain for the agent-use benchmark (scripts/bench/agent-use): Mewla's extension in a Chrome for
// Testing, the native host, the daemon's bridge and the tools' argv exactly as MCPServer builds it. The test is not
// a check: it brings the chain up, writes how to reach it to AUB_BRIDGE_OUT, and keeps it up until
// AUB_BRIDGE_OUT + ".stop" exists. A benchmark run then drives the browser tools through that argv while a
// simulated person uses another window of the same Chrome.
//
//	DISPLAY=:97 AUB_BRIDGE_OUT=/tmp/x/bridge.json MEWLA_E2E_CHROME=<chrome> MEWLA_E2E_MCP=<@playwright/mcp/cli.js> \
//	  MEWLA_E2E_BIN=<mewla binary> go test ./agentuse -run AgentUseBenchBridge -v -count=1 -timeout 3h
//
// It is skipped unless those are set, like TestMewlaExtensionE2E, whose setup it repeats.
func TestAgentUseBenchBridge(t *testing.T) {
	out := os.Getenv("AUB_BRIDGE_OUT")
	chromeBin, mcpCLI, mewla := os.Getenv("MEWLA_E2E_CHROME"), os.Getenv("MEWLA_E2E_MCP"), os.Getenv("MEWLA_E2E_BIN")
	if out == "" || chromeBin == "" || mcpCLI == "" || mewla == "" || (runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "") {
		t.Skip("set AUB_BRIDGE_OUT, MEWLA_E2E_CHROME, MEWLA_E2E_MCP and MEWLA_E2E_BIN (and DISPLAY on Linux)")
	}
	root, err := os.MkdirTemp("", "aub-bridge")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	state := filepath.Join(root, "state")
	profile := filepath.Join(root, "chrome")

	svc, err := New(state, true)
	if err != nil {
		t.Fatal(err)
	}
	svc.lookChrome = func(string) (chromeInstall, bool) { return chromeInstall{chromeBin, profile}, true }
	svc.lookDesktop = func() ([]string, bool) { return []string{"DISPLAY=" + os.Getenv("DISPLAY")}, true }
	svc.executable = func() (string, error) { return mewla, nil }
	extDir, err := svc.ExtensionDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.InstallNativeHost(profile, mewla, []string{MewlaExtensionID}); err != nil {
		t.Fatal(err)
	}
	pkg := svc.packageDir()
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(filepath.Dir(filepath.Dir(mcpCLI))), filepath.Join(pkg, "node_modules")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pkg, ".mewla-installed"), []byte(lockStamp()+"\n"), 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge := svc.Bridge()
	go func() {
		if err := bridge.Serve(ctx); err != nil {
			t.Errorf("the bridge can't listen: %v", err)
		}
	}()

	chrome := exec.Command(chromeBin, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port=9334", "--load-extension="+extDir, "--window-size=900,700", "about:blank")
	if runtime.GOOS == "linux" {
		chrome.Args = append(chrome.Args, "--no-sandbox")
		chrome.Env = append(os.Environ(), "HOME="+root)
		chrome.Stderr = os.Stderr
	}
	if err := chrome.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chrome.Process.Kill(); _ = chrome.Wait() }()
	for i := 0; i < 100 && !bridge.HasHost(); i++ {
		time.Sleep(200 * time.Millisecond)
	}
	if !bridge.HasHost() {
		t.Fatal("Mewla's extension never connected through its native host")
	}
	if err := os.MkdirAll(filepath.Join(profile, "Default", "Extensions", MewlaExtensionID), 0o755); err != nil {
		t.Fatal(err)
	}
	status := svc.Refresh()
	if status.Browser.State != BrowserReady {
		t.Fatalf("browser status %+v", status.Browser)
	}
	argv, env, err := svc.MCPServer()
	if err != nil {
		t.Fatal(err)
	}
	argv[1] = mcpCLI
	if !strings.Contains(strings.Join(argv, " "), "chrome-for-tools.sh") {
		t.Fatalf("the tools weren't pointed at Mewla's stand-in for Chrome: %v", argv)
	}
	// MCPServer hands back the whole environment of this process; a benchmark run needs only what Chrome and the
	// tools read, and the descriptor must not carry the runner's credentials.
	keep := map[string]bool{"PATH": true, "HOME": true, "DISPLAY": true, "XAUTHORITY": true, "XDG_RUNTIME_DIR": true,
		"DBUS_SESSION_BUS_ADDRESS": true, "WAYLAND_DISPLAY": true, "LANG": true, "TMPDIR": true, "TERM": true}
	var safe []string
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if keep[name] || strings.HasPrefix(name, "MEWLA_RELAY") || strings.HasPrefix(name, "PLAYWRIGHT_") {
			safe = append(safe, entry)
		}
	}
	raw, _ := json.Marshal(map[string]any{"argv": argv, "env": safe, "cdp": 9334, "extension_version": bridge.HostVersion()})
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, out); err != nil {
		t.Fatal(err)
	}
	t.Logf("the chain is up (extension %s); remove nothing, touch %s.stop to end", bridge.HostVersion(), out)
	for {
		if _, err := os.Stat(out + ".stop"); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
