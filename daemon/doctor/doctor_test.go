package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/work"
)

func TestRunCleanPathNotReadyButValidJSON(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	stateDir := filepath.Join(home, ".mewla")

	report, err := Run(Options{
		Home:     home,
		StateDir: stateDir,
		Addr:     "127.0.0.1:0",
		PathEnv:  binDir,
		Now:      func() time.Time { return time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Ready {
		t.Fatal("expected not ready on clean PATH")
	}
	if report.Executors.Status != StatusFail || report.Executors.Remediation != RemediationInstallExecutor {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.UsableCount != 0 {
		t.Fatalf("usable_count = %d", report.Executors.UsableCount)
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"ready", "platform", "state_dir", "listen", "executors", "checks", "remediations"} {
		if _, ok := roundtrip[key]; !ok {
			t.Fatalf("missing JSON key %q in %s", key, raw)
		}
	}
	if strings.Contains(string(raw), "sk-") {
		t.Fatalf("JSON leaked secret-like token: %s", raw)
	}

	var human bytes.Buffer
	if err := WriteHuman(&human, report); err != nil {
		t.Fatalf("WriteHuman: %v", err)
	}
	text := human.String()
	for _, want := range []string{
		"NOT READY",
		"install_executor",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output missing %q:\n%s", want, text)
		}
	}
}

func TestRunReadyWithAnInstalledExecutor(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	stateDir := filepath.Join(home, ".mewla")
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeExecutorsTOML(t, home, `delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"
`)

	report, err := Run(Options{
		Home:          home,
		StateDir:      stateDir,
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Now:           func() time.Time { return time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Ready {
		t.Fatalf("expected ready, report=%+v", report)
	}
	if report.StateDir.Status != StatusOK || report.StateDir.Path != stateDir {
		t.Fatalf("state_dir = %+v", report.StateDir)
	}
	if report.Executors.UsableCount < 1 || report.Executors.RecommendedHost != "codex" {
		t.Fatalf("executors = %+v", report.Executors)
	}
	codex := findExecutor(report, "codex")
	if codex == nil || !codex.Usable || !codex.Runnable || codex.Status != StatusOK {
		t.Fatalf("codex = %+v", codex)
	}
	if report.Executors.Status != StatusOK {
		t.Fatalf("executors = %+v", report.Executors)
	}
}

func TestGrokOnlyReady(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeGrok(t, binDir)
	writeExecutorsTOML(t, home, `delegated_executor = "grok"

[[executors]]
name = "grok"
command = "grok"
`)

	report := mustRunDoctor(t, home, binDir)
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report)
	}
	if report.Executors.Status != StatusOK || report.Executors.UsableCount < 1 {
		t.Fatalf("executors = %+v", report.Executors)
	}
	grok := findExecutor(report, "grok")
	if grok == nil || !grok.Usable {
		t.Fatalf("grok = %+v", grok)
	}
	if report.Executors.RecommendedHost != "grok" {
		t.Fatalf("host=%s", report.Executors.RecommendedHost)
	}
	var human bytes.Buffer
	_ = WriteHuman(&human, report)
	if text := strings.ToLower(human.String()); strings.Contains(text, "auth=") || strings.Contains(text, "signed in") || strings.Contains(text, "verified") {
		t.Fatalf("human output speaks of sign-in:\n%s", human.String())
	}
}

func TestCustomExecutorReady(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeCustom(t, binDir, "mycli")
	writeExecutorsTOML(t, home, `delegated_executor = "mycli"

[[executors]]
name = "mycli"
command = "mycli"
`)

	report := mustRunDoctor(t, home, binDir)
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report)
	}
	item := findExecutor(report, "mycli")
	if item == nil || item.Provider != "custom" || !item.Usable {
		t.Fatalf("mycli = %+v", item)
	}
	if report.Executors.Status != StatusOK {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.RecommendedHost != "mycli" {
		t.Fatalf("recommended host = %q", report.Executors.RecommendedHost)
	}
}

func TestRecommendedHostFollowsTheInstallMenuOrder(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeClaude(t, binDir, `{"loggedIn": true}`)
	writeExecutorsTOML(t, home, `[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "claude"
command = "claude"
`)

	report := mustRunDoctor(t, home, binDir)
	if report.Executors.UsableCount != 2 {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.RecommendedHost != "claude" {
		t.Fatalf("recommended host = %q, want claude, first as on the install menu", report.Executors.RecommendedHost)
	}
	if order := hostdeps.AgentOrder(); recommendHost([]ExecutorCheck{{ID: "opencode"}, {ID: order[1]}}) != order[1] {
		t.Fatalf("recommendHost does not follow hostdeps.AgentOrder %v", order)
	}
	custom := []ExecutorCheck{{ID: "my-grok", Provider: work.WorkerProviderGrok}, {ID: "my-cursor", Provider: work.WorkerProviderCursor}}
	if got := recommendHost(custom); got != "my-cursor" {
		t.Fatalf("custom ids rank by provider: got %q, want my-cursor (Cursor Agent before Grok)", got)
	}
	for _, id := range hostdeps.AgentOrder() {
		if agentProviders[id] == "" {
			t.Fatalf("no provider for %q", id)
		}
	}
}

func writeFakeClaude(t *testing.T, binDir, authStatus string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "2.0.0 (Claude Code)"
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  printf '%%s\n' %q
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`, authStatus)
	writeExec(t, filepath.Join(binDir, "claude"), script)
}

func TestRetiredDelegatedExecutorKeyIsReported(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeGrok(t, binDir)
	writeExecutorsTOML(t, home, `delegated_executor = "grok"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok"
`)

	report := mustRunDoctor(t, home, binDir)
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report)
	}
	if report.Executors.Status != StatusOK {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.RecommendedHost != "codex" {
		t.Fatalf("recommended host = %q, want codex before grok, as on the install menu", report.Executors.RecommendedHost)
	}
	// The retired delegated_executor key loads, is ignored and is reported.
	if !strings.Contains(strings.Join(report.Executors.Warnings, "\n"), "delegated_executor are retired and ignored") {
		t.Fatalf("missing retired-key warning: %v", report.Executors.Warnings)
	}
	grok := findExecutor(report, "grok")
	if grok == nil || !grok.Usable {
		t.Fatalf("grok should remain usable: %+v", grok)
	}
}

func TestDetectsAlreadyRunningDaemon(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".mewla"),
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          "127.0.0.1:19876",
		PathEnv:       binDir,
		Listen: func(network, address string) (io.Closer, error) {
			t.Fatal("Listen should not be called when Mewla health succeeds")
			return nil, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 200, []byte(`{"status":"ok","daemon_id":"abc123"}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Listen.DaemonRunning || report.Listen.DaemonID != "abc123" || report.Listen.Status != StatusOK {
		t.Fatalf("listen = %+v", report.Listen)
	}
	if !report.Ready {
		t.Fatalf("expected ready when Mewla already running: %+v", report)
	}
}

func TestPortInUseByNonMewla(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".mewla"),
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          ln.Addr().String(),
		PathEnv:       binDir,
		HTTPGet:       func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Listen.Status != StatusFail || report.Listen.Remediation != RemediationPortInUse {
		t.Fatalf("listen = %+v", report.Listen)
	}
	if report.Ready {
		t.Fatal("expected not ready when port is blocked")
	}
}

func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("exit %d produced %v", code, err)
	}
	return err
}

func TestPlatformSupportedOnCurrentOS(t *testing.T) {
	report, err := Run(Options{
		Home:     t.TempDir(),
		StateDir: t.TempDir(),
		PathEnv:  t.TempDir(),
		Listen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:  func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		if !report.Platform.Supported || report.Platform.Status != StatusOK {
			t.Fatalf("platform = %+v", report.Platform)
		}
	default:
		if report.Platform.Supported || report.Platform.Status != StatusFail {
			t.Fatalf("platform = %+v", report.Platform)
		}
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func mustRunDoctor(t *testing.T, home, binDir string) Report {
	t.Helper()
	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".mewla"),
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Listen:        func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:       func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

func writeExecutorsTOML(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".mewla")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executors.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeFakeCodex(t *testing.T, binDir, loginStatus string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "codex-cli 0.0.1-test"
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  # Like the real CLI: exit 0 when logged in, exit 1 saying so when not.
  if [ %q = "Not logged in" ]; then
    echo %q >&2
    exit 1
  fi
  printf '%%s\n' %q
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`, loginStatus, loginStatus, loginStatus)
	writeExec(t, filepath.Join(binDir, "codex"), script)
}

func writeFakeGrok(t *testing.T, binDir string) {
	t.Helper()
	script := `#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "grok 0.0.1-test"
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`
	writeExec(t, filepath.Join(binDir, "grok"), script)
}

func writeFakeCustom(t *testing.T, binDir, name string) {
	t.Helper()
	script := `#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "mycli 1.0.0-test"
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`
	writeExec(t, filepath.Join(binDir, name), script)
}

func TestParseOpenCodeModelsAndServiceProbes(t *testing.T) {
	if got := parseOpenCodeModelsProbe([]byte("openai/gpt-5\nopencode/big-pickle\n"), nil); got != StatusOK {
		t.Fatalf("models ok = %s", got)
	}
	if got := parseOpenCodeModelsProbe(nil, errors.New("exit 1")); got != StatusFail {
		t.Fatalf("models err = %s", got)
	}
	if got := parseOpenCodeModelsProbe([]byte("\n\n"), nil); got != StatusUnknown {
		t.Fatalf("models empty = %s", got)
	}
	if got := parseOpenCodeServiceProbe([]byte("http://127.0.0.1:49374\n"), nil); got != StatusOK {
		t.Fatalf("service ok = %s", got)
	}
	if got := parseOpenCodeServiceProbe(nil, errors.New("exit 1")); got != StatusFail {
		t.Fatalf("service err = %s", got)
	}
	if got := parseOpenCodeServiceProbe([]byte("not running\n"), nil); got != StatusUnknown {
		t.Fatalf("service stopped = %s", got)
	}
}

func TestOpenCodeDoctorProbesAreSecretSafeAndNonMutating(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	stateDir := filepath.Join(home, ".mewla")
	serviceURL := "http://127.0.0.1:49374"
	writeFakeOpenCode(t, binDir, openCodeFakeBehavior{
		authList:   `[{"id":"openai","name":"OpenAI","connections":[{"type":"credential","id":"c1","label":"oauth","method":"oauth"}]}]`,
		models:     "openai/gpt-5\nopencode/big-pickle\n",
		serviceURL: serviceURL,
	})
	writeExecutorsTOML(t, home, `
delegated_executor = "opencode"

[[executors]]
name = "opencode"
command = "opencode"
`)

	var seen []string
	report, err := Run(Options{
		Home:          home,
		StateDir:      stateDir,
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Now:           func() time.Time { return time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			seen = append(seen, strings.Join(append([]string{filepath.Base(name)}, args...), " "))
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Env = append(os.Environ(), "PATH="+binDir)
			return cmd.CombinedOutput()
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	item := findExecutor(report, "opencode")
	if item == nil {
		t.Fatal("missing opencode executor check")
	}
	if item.ModelsStatus != StatusOK || item.ServiceStatus != StatusOK {
		t.Fatalf("probe status models=%s service=%s summary=%q", item.ModelsStatus, item.ServiceStatus, item.Summary)
	}
	joined := strings.Join(seen, " | ")
	if strings.Contains(joined, "auth") {
		t.Fatalf("doctor asked OpenCode about its sign-in: %v", seen)
	}
	if !strings.Contains(joined, "opencode models") || !strings.Contains(joined, "opencode service status") {
		t.Fatalf("expected models and service status probes, seen=%v", seen)
	}
	for _, call := range seen {
		if strings.Contains(call, "service start") || strings.Contains(call, "service stop") {
			t.Fatalf("doctor must not change the service: %q", call)
		}
	}
	for _, call := range seen {
		if strings.Contains(call, "--refresh") {
			t.Fatalf("models probe must not refresh: %q", call)
		}
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "openai/gpt-5") || strings.Contains(string(raw), "oauth") {
		t.Fatalf("report leaked probe body: %s", raw)
	}
	if strings.Contains(item.Summary, "openai/gpt-5") || strings.Contains(item.Summary, serviceURL) {
		t.Fatalf("summary leaked probe body: %q", item.Summary)
	}
}

func TestOpenCodeDoctorProbeFailuresStayHonest(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	stateDir := filepath.Join(home, ".mewla")
	writeFakeOpenCode(t, binDir, openCodeFakeBehavior{
		authList:    "",
		modelsFail:  true,
		serviceFail: true,
	})
	writeExecutorsTOML(t, home, `
[[executors]]
name = "opencode"
command = "opencode"
`)
	report, err := Run(Options{
		Home:          home,
		StateDir:      stateDir,
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Now:           func() time.Time { return time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	item := findExecutor(report, "opencode")
	if item == nil {
		t.Fatal("missing opencode executor check")
	}
	if item.ModelsStatus != StatusFail || item.ServiceStatus != StatusFail {
		t.Fatalf("expected failed probes, got models=%s service=%s", item.ModelsStatus, item.ServiceStatus)
	}
	if !strings.Contains(item.Summary, "models probe failed") || !strings.Contains(item.Summary, "service probe failed") {
		t.Fatalf("summary = %q", item.Summary)
	}
}

type openCodeFakeBehavior struct {
	version     string
	authList    string
	models      string
	serviceURL  string
	modelsFail  bool
	serviceFail bool
}

func writeFakeOpenCode(t *testing.T, binDir string, behavior openCodeFakeBehavior) {
	t.Helper()
	version := behavior.version
	if version == "" {
		version = "opencode v2.0.32"
	}
	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo %q
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "list" ]; then
  printf '%%s' %q
  exit 0
fi
if [ "$1" = "models" ]; then
  if [ %q = "1" ]; then
    echo "models failed" >&2
    exit 1
  fi
  for arg in "$@"; do
    if [ "$arg" = "--refresh" ]; then
      echo "refresh refused" >&2
      exit 2
    fi
  done
  printf '%%s' %q
  exit 0
fi
if [ "$1" = "service" ] && [ "$2" = "status" ]; then
  if [ %q = "1" ]; then
    echo "service failed" >&2
    exit 1
  fi
  printf '%%s\n' %q
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`, version,
		behavior.authList,
		bool01(behavior.modelsFail),
		behavior.models,
		bool01(behavior.serviceFail),
		behavior.serviceURL,
	)
	writeExec(t, filepath.Join(binDir, "opencode"), script)
}

func bool01(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func findExecutor(report Report, id string) *ExecutorCheck {
	for i := range report.Executors.Items {
		if report.Executors.Items[i].ID == id {
			return &report.Executors.Items[i]
		}
	}
	return nil
}

func TestOpenCodeDoctorRejectsOpenCode1(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeOpenCode(t, binDir, openCodeFakeBehavior{version: "1.18.13", models: "openai/gpt-5\n"})
	writeExecutorsTOML(t, home, `
[[executors]]
name = "opencode"
command = "opencode"
`)
	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".mewla"),
		ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Now:           func() time.Time { return time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	item := findExecutor(report, "opencode")
	if item == nil {
		t.Fatal("missing opencode executor check")
	}
	if item.Status != StatusFail || item.Remediation != RemediationInstallExecutor {
		t.Fatalf("status=%s remediation=%s", item.Status, item.Remediation)
	}
	if !strings.Contains(item.Summary, "OpenCode 1.18.13 found") || !strings.Contains(item.Summary, "OpenCode 2 only") {
		t.Fatalf("summary = %q", item.Summary)
	}
}

func TestProbeVersionAsksOncePerBuildOfABinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	asked := 0
	e := newEnv(Options{RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		asked++
		return []byte("agent 1.2.3\n"), nil
	}})
	if got := e.probeVersion(path, "claude"); got == "" {
		t.Fatal("no version")
	}
	e.probeVersion(path, "claude")
	if asked != 1 {
		t.Fatalf("the same build was asked %d times, want 1", asked)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	e.probeVersion(path, "claude")
	if asked != 2 {
		t.Fatalf("a replaced binary was asked %d times in all, want 2", asked)
	}
	// A path that cannot be examined is never remembered.
	e.probeVersion(filepath.Join(t.TempDir(), "missing"), "claude")
	e.probeVersion(filepath.Join(t.TempDir(), "missing"), "claude")
	if asked != 4 {
		t.Fatalf("unexaminable paths were asked %d times in all, want 4", asked)
	}
}

// Doctor never asks an agent whether it is signed in: a "Not logged in" Codex
// is as usable as any installed agent, and no login command runs.
func TestSignInIsNeverChecked(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	writeExec(t, filepath.Join(binDir, "codex"), fmt.Sprintf(`#!/bin/sh
echo "$*" >> %q
if [ "$1" = "--version" ]; then echo "codex-cli 0.1.0"; exit 0; fi
echo "Not logged in"
exit 1
`, calls))
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)
	report := mustRunDoctor(t, home, binDir)
	codex := findExecutor(report, "codex")
	if codex == nil || !codex.Usable || !report.Ready || report.Executors.Status != StatusOK {
		t.Fatalf("codex = %+v ready=%v", codex, report.Ready)
	}
	raw, _ := os.ReadFile(calls)
	if strings.Contains(string(raw), "login") || strings.Contains(string(raw), "auth") {
		t.Fatalf("doctor ran a sign-in check: %q", raw)
	}
	encoded, _ := json.Marshal(report.Executors)
	if text := string(encoded); strings.Contains(text, "auth") || strings.Contains(text, "signed") || strings.Contains(text, "verified") {
		t.Fatalf("executors report carries sign-in: %s", text)
	}
}
