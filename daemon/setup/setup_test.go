package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/work"
)

func TestSetupMissingTmuxStopsCleanly(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	var out bytes.Buffer
	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Home:           home,
		StateDir:       filepath.Join(home, ".mewla"),
		ExecutorsPath:  filepath.Join(home, ".mewla", "executors.toml"),
		Addr:           "127.0.0.1:0",
		PathEnv:        binDir,
		Stdout:         &out,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
	if result.OK || !result.StoppedEarly || result.Step != "machine" {
		t.Fatalf("result = %+v", result)
	}
	text := out.String()
	for _, want := range []string{"not ready", "tmux", "Install tmux on ", "mewla deps -only tmux", "mewla setup"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".mewla", "executors.toml")); !os.IsNotExist(err) {
		t.Fatalf("config should not be written on blocked machine: %v", err)
	}
}

func TestSetupNoExecutorStopsWithLoginHints(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	var out bytes.Buffer
	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Home:           home,
		StateDir:       filepath.Join(home, ".mewla"),
		ExecutorsPath:  filepath.Join(home, ".mewla", "executors.toml"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         &out,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if !errors.Is(err, ErrNoExecutor) {
		t.Fatalf("err = %v", err)
	}
	if result.OK {
		t.Fatal("expected not ok")
	}
	text := out.String()
	for _, want := range []string{"no runnable executor", "codex login", "claude auth login", "cursor-agent login", "grok login"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

func TestSetupVerifiedBeforeUnknownOrderingAndWrite(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeGrok(t, binDir)
	writeExecutorsSeed(t, home, `
delegated_executor = "grok"

[[executors]]
name = "custom-keep"
command = "echo keep-me"

[[executors]]
name = "codex"
command = "codex --dangerously-bypass-approvals-and-sandbox"

[[executors]]
name = "grok"
command = "grok --no-alt-screen --permission-mode bypassPermissions"
`)

	var out bytes.Buffer
	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Home:           home,
		StateDir:       filepath.Join(home, ".mewla"),
		ExecutorsPath:  filepath.Join(home, ".mewla", "executors.toml"),
		BrainRoot:      filepath.Join(home, ".mewla", "brain"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         &out,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK || !result.BrainConfigured {
		t.Fatalf("result = %+v", result)
	}
	candidates := selectableCandidates(result.Doctor)
	if len(candidates) < 2 || !candidates[0].VerifiedAuthenticated || candidates[0].ID != "codex" {
		t.Fatalf("candidate order = %+v", candidates)
	}

	raw, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `command = "grok --no-alt-screen --permission-mode bypassPermissions"`) {
		t.Fatalf("unselected executor's full-authority command was not kept:\n%s", text)
	}
	if strings.Contains(text, "Safe") || strings.Contains(text, "Autonomous") || strings.Contains(text, "Profile:") {
		t.Fatalf("config still describes a permission profile:\n%s", text)
	}
	if !strings.Contains(text, "custom-keep") || !strings.Contains(text, "echo keep-me") {
		t.Fatalf("unrelated executor not preserved:\n%s", text)
	}
	if result.BackupPath == "" {
		t.Fatal("expected backup path")
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestSetupConfiguresBrainAndFullAuthority(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeCursor(t, binDir)

	result, err := Run(Options{
		NonInteractive: true,
		Host:           "agent",
		Home:           home,
		StateDir:       filepath.Join(home, ".mewla"),
		ExecutorsPath:  filepath.Join(home, ".mewla", "executors.toml"),
		BrainRoot:      filepath.Join(home, ".mewla", "brain"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK || !result.BrainConfigured {
		t.Fatalf("result = %+v", result)
	}
	raw, _ := os.ReadFile(result.ConfigPath)
	if !strings.Contains(string(raw), "cursor-agent --force --sandbox disabled") {
		t.Fatalf("full-authority cursor command missing:\n%s", raw)
	}
	store, err := brain.NewStore(filepath.Join(home, ".mewla", "brain"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatalf("HostSession: %v", err)
	}
	if host.ExecutorID != "agent" {
		t.Fatalf("brain host = %#v", host)
	}
}

func TestSetupPreservesBackupAndIdempotentRerun(t *testing.T) {
	home := t.TempDir()
	binDir := readyBinDir(t)
	path := filepath.Join(home, ".mewla", "executors.toml")
	writeExecutorsSeed(t, home, `
delegated_executor = "codex"
[[executors]]
name = "codex"
command = "codex"
[[executors]]
name = "keep"
command = "keep-cmd"
`)

	first, err := Run(baseOpts(home, binDir, "codex"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := Run(baseOpts(home, binDir, "codex"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.BackupPath == "" || second.BackupPath == "" {
		t.Fatalf("expected backups: first=%q second=%q", first.BackupPath, second.BackupPath)
	}
	if first.BackupPath == second.BackupPath {
		// timestamps could collide in fixedNow — use advancing now for second run
	}
	var parsed struct {
		DelegatedExecutor string `toml:"delegated_executor"`
		Executors         []struct {
			Name    string `toml:"name"`
			Command string `toml:"command"`
		} `toml:"executors"`
	}
	raw, _ := os.ReadFile(path)
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("toml: %v", err)
	}
	// Setup rewrites drop the retired routing key (the backup keeps it).
	if parsed.DelegatedExecutor != "" {
		t.Fatalf("retired delegated_executor rewritten = %q", parsed.DelegatedExecutor)
	}
	foundKeep := false
	for _, e := range parsed.Executors {
		if e.Name == "keep" && e.Command == "keep-cmd" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatalf("keep entry missing:\n%s", raw)
	}
}

func TestSetupNonInteractiveInvalidArgs(t *testing.T) {
	home := t.TempDir()
	binDir := readyBinDir(t)
	_, err := Run(Options{
		NonInteractive: true,
		Home:           home,
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	})
	if !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("err = %v", err)
	}

	_, err = Run(baseOpts(home, binDir, "not-a-candidate"))
	if !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("bad host err = %v", err)
	}
}

func TestSetupDoesNotEmitSecrets(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using an API key - sk-SECRETVALUE")
	var out bytes.Buffer
	opts := baseOpts(home, binDir, "codex")
	opts.Stdout = &out
	result, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v\nout=%s", err, out.String())
	}
	blob := out.String() + result.Message + strings.Join(result.Warnings, "") + strings.Join(result.NextSteps, "")
	if strings.Contains(blob, "sk-") || strings.Contains(blob, "SECRET") {
		t.Fatalf("secret leaked: %s", blob)
	}
	raw, _ := os.ReadFile(result.ConfigPath)
	if strings.Contains(string(raw), "sk-") {
		t.Fatalf("secret in config: %s", raw)
	}
}

func TestNextStepsKeepPrivateAndHTTPSRoutesDistinct(t *testing.T) {
	steps := strings.Join(nextSteps("/tmp/mewla-state"), "\n")
	for _, want := range []string{
		"mewla --lan",
		"direct Tailnet",
		"start Mewla with mewla serve, expose the full loopback origin",
		"mewla address add https://<your-host>, then mewla pair -state-dir /tmp/mewla-state",
	} {
		if !strings.Contains(steps, want) {
			t.Fatalf("next steps missing %q:\n%s", want, steps)
		}
	}
	if strings.Contains(steps, "mewla pair http://0.0.0.0") {
		t.Fatalf("next steps contain wildcard pair command:\n%s", steps)
	}
}

func TestCommandForProviderGivesEveryAgentFullAuthority(t *testing.T) {
	cmd, _ := commandForProvider("custom", "mycli", "mycli --fancy")
	if cmd != "mycli --fancy" {
		t.Fatalf("custom command mutated: %q", cmd)
	}
	for provider, want := range map[string]string{
		work.WorkerProviderCursor:   "cursor-agent --force --sandbox disabled",
		work.WorkerProviderGrok:     "grok --no-alt-screen --permission-mode bypassPermissions",
		work.WorkerProviderOpenCode: "opencode --auto",
	} {
		if got, _ := commandForProvider(provider, provider, ""); got != want {
			t.Fatalf("%s command = %q, want %q", provider, got, want)
		}
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func fixedNow() time.Time {
	return time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
}

func baseOpts(home, binDir, host string) Options {
	return Options{
		NonInteractive: true,
		Host:           host,
		Home:           home,
		StateDir:       filepath.Join(home, ".mewla"),
		ExecutorsPath:  filepath.Join(home, ".mewla", "executors.toml"),
		BrainRoot:      filepath.Join(home, ".mewla", "brain"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	}
}

func readyBinDir(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	return binDir
}

func writeExecutorsSeed(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".mewla")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executors.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeFakeTmux(t *testing.T, binDir string) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
case "$1" in
  -V) echo "tmux 3.4"; exit 0 ;;
  new-session|kill-session) exit 0 ;;
  *) exit 1 ;;
esac
`)
}

func writeFakeCodex(t *testing.T, binDir, loginStatus string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "codex-cli 0.0.1"; exit 0; fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then printf '%%s\n' %q; exit 0; fi
exit 1
`, loginStatus)
	writeExec(t, filepath.Join(binDir, "codex"), script)
}

func writeFakeGrok(t *testing.T, binDir string) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "grok"), `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "grok 0.0.1"; exit 0; fi
exit 1
`)
}

func writeFakeCursor(t *testing.T, binDir string) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "cursor-agent"), `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "2026.01.01"; exit 0; fi
if [ "$1" = "status" ]; then echo '{"status":"authenticated","isAuthenticated":true}'; exit 0; fi
exit 1
`)
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// Capability reporting looks for hostdeps.AgentCommand(id); that must be the
// executable the executor registry launches for the same agent.
func TestAgentCommandIsWhatTheExecutorRunsByDefault(t *testing.T) {
	providers := map[string]string{
		"claude":   work.WorkerProviderClaude,
		"codex":    work.WorkerProviderCodex,
		"agent":    work.WorkerProviderCursor,
		"grok":     work.WorkerProviderGrok,
		"pi":       work.WorkerProviderPi,
		"opencode": work.WorkerProviderOpenCode,
	}
	for _, id := range hostdeps.AgentOrder() {
		provider, ok := providers[id]
		if !ok {
			t.Fatalf("agent %q has no provider here", id)
		}
		command, _ := commandForProvider(provider, id, "")
		if fields := strings.Fields(command); len(fields) == 0 || fields[0] != hostdeps.AgentCommand(id) {
			t.Fatalf("agent %q: registry runs %q, caps look for %q", id, command, hostdeps.AgentCommand(id))
		}
	}
}
