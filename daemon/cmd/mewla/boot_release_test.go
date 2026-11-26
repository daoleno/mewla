package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/modelprofiles"
)

const releaseUserCodexConfig = `model = "gpt-6"
model_provider = "freeride"

[model_providers.freeride]
name = "freeride"
base_url = "https://freeride.example/v1"
`

// releaseProjected puts Codex and Claude Code on the gateway as a running daemon would, under home's default paths.
func releaseProjected(t *testing.T, home, stateDir string) (codexConfig, claudeSettings string) {
	t.Helper()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("MEWLA_NO_CONFIG_TAKEOVER", "")
	codexConfig = filepath.Join(home, ".codex", "config.toml")
	claudeSettings = filepath.Join(home, ".claude", "settings.json")
	for _, dir := range []string{filepath.Dir(codexConfig), filepath.Dir(claudeSettings)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(codexConfig, []byte(releaseUserCodexConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeSettings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://mine.example"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gatewayDir := filepath.Join(stateDir, "codex-gateway")
	if err := os.MkdirAll(gatewayDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := modelprofiles.NewTakeover(codexConfig, gatewayDir, nil).Enable("127.0.0.1:3425"); err != nil {
		t.Fatal(err)
	}
	if err := modelprofiles.NewClaudeTakeover(claudeSettings, gatewayDir).Project("http://127.0.0.1:3425", true, true); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(codexConfig); !strings.Contains(string(raw), `model_provider = "`+modelprofiles.GatewayProviderName+`"`) {
		t.Fatalf("setup: Codex not projected:\n%s", raw)
	}
	return codexConfig, claudeSettings
}

func releaseClaudeBaseURL(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	return settings.Env["ANTHROPIC_BASE_URL"]
}

func TestBootUninstallGivesCodexAndClaudeTheirOwnSettingsBack(t *testing.T) {
	config, home := newBootTestEnvironment(t)
	codexConfig, claudeSettings := releaseProjected(t, home, config.StateDir)
	bootWriteInstalledUnit(t, config)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	var out bytes.Buffer
	if err := bootUninstall(runner, &out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(codexConfig)
	if !strings.Contains(string(raw), `model_provider = "freeride"`) || strings.Contains(string(raw), modelprofiles.GatewayProviderName) {
		t.Fatalf("Codex config after uninstall:\n%s", raw)
	}
	if got := releaseClaudeBaseURL(t, claudeSettings); got != "https://mine.example" {
		t.Fatalf("Claude Code base URL after uninstall = %q", got)
	}
}

func TestBootUninstallLeavesSettingsToAnotherOwner(t *testing.T) {
	config, home := newBootTestEnvironment(t)
	bootUseProcFixture(t, os.Getpid())
	codexConfig, claudeSettings := releaseProjected(t, home, config.StateDir)
	bootWriteInstalledUnit(t, config)
	bootHoldStateLock(t, config.StateDir)
	runner := bootTestRunner()
	runner.set(bootIsActive, "inactive")
	runner.set(bootControlGroup, "/user.slice/user-1000.slice/user@1000.service/app.slice/manual-owner.service")
	if err := bootUninstall(runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(codexConfig); !strings.Contains(string(raw), `model_provider = "`+modelprofiles.GatewayProviderName+`"`) {
		t.Fatalf("a daemon still running on this state lost its Codex projection:\n%s", raw)
	}
	if got := releaseClaudeBaseURL(t, claudeSettings); got != "http://127.0.0.1:3425" {
		t.Fatalf("a daemon still running on this state lost its Claude projection: %q", got)
	}
}

func TestBootLaunchdUninstallGivesSettingsBack(t *testing.T) {
	config, runner := launchdFreshInstall(t)
	if err := bootLaunchdInstall(config, runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	daemon := bootLaunchdDaemonConfig(config)
	codexConfig, claudeSettings := releaseProjected(t, config.Home, daemon.StateDir)
	runner.reset()
	if err := bootLaunchdUninstall(runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(codexConfig); !strings.Contains(string(raw), `model_provider = "freeride"`) {
		t.Fatalf("Codex config after uninstall:\n%s", raw)
	}
	if got := releaseClaudeBaseURL(t, claudeSettings); got != "https://mine.example" {
		t.Fatalf("Claude Code base URL after uninstall = %q", got)
	}
}

func TestBootTaskStateDirReadsTheDaemonArguments(t *testing.T) {
	for xml, want := range map[string]string{
		`<Arguments>--headless C:\mewla.exe -state-dir C:\Users\me\.mewla -addr 127.0.0.1:9877</Arguments>`:              `C:\Users\me\.mewla`,
		`<Arguments>--headless "C:\Program Files\mewla.exe" -state-dir &quot;C:\Users\A B\.mewla&quot; -lan</Arguments>`: `C:\Users\A B\.mewla`,
		`<Arguments>--headless C:\mewla.exe -addr 127.0.0.1:9877</Arguments>`:                                            "",
	} {
		if got := bootTaskStateDir(xml); got != want {
			t.Errorf("bootTaskStateDir(%s) = %q, want %q", xml, got, want)
		}
	}
}

func TestBootGiveBackLeavesARunningDaemonItsProjection(t *testing.T) {
	config, home := newBootTestEnvironment(t)
	codexConfig, _ := releaseProjected(t, home, config.StateDir)
	// A daemon that still runs keeps the projection; give-back says so.
	bootHoldStateLock(t, config.StateDir)
	var out bytes.Buffer
	if err := bootGiveBack(config.StateDir, 300*time.Millisecond, &out); err == nil || !strings.Contains(out.String(), "still runs") {
		t.Fatalf("give-back with the daemon running: %v, %q", err, out.String())
	}
	if raw, _ := os.ReadFile(codexConfig); !strings.Contains(string(raw), modelprofiles.GatewayProviderName) {
		t.Fatalf("a running daemon's projection was given back:\n%s", raw)
	}
}

// launchd and schtasks return before the daemon has exited: give-back waits for its state lock.
func TestBootGiveBackWaitsForTheDaemonToStop(t *testing.T) {
	config, home := newBootTestEnvironment(t)
	codexConfig, claudeSettings := releaseProjected(t, home, config.StateDir)
	lock, acquired, err := control.TryAcquireLifecycleLock(config.StateDir)
	if err != nil || !acquired {
		t.Fatalf("hold state lock: %t %v", acquired, err)
	}
	time.AfterFunc(300*time.Millisecond, func() { _ = lock.Close() })
	if err := bootGiveBack(config.StateDir, 5*time.Second, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(codexConfig); !strings.Contains(string(raw), `model_provider = "freeride"`) {
		t.Fatalf("Codex config after give-back:\n%s", raw)
	}
	if got := releaseClaudeBaseURL(t, claudeSettings); got != "https://mine.example" {
		t.Fatalf("Claude Code base URL after give-back = %q", got)
	}
}
