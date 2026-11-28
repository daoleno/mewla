package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/work"
)

// The disposable Windows runner executes real PowerShell launchers. Cursor's
// script stands in for an installed cursor-agent wrapper.
func TestBrainRefreshDoesNotLaunchUnusedWindowsAgents(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "launches.txt")
	configPath := filepath.Join(dir, "executors.toml")
	config := "[[executors]]\nname = 'cursor-alias'\ncommand = 'cursor-agent'\nkind = 'cursor'\n"
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	script := `$ErrorActionPreference = 'Stop'
Add-Content -LiteralPath $env:MEWLA_TEST_COUNT -Value $env:MEWLA_TEST_AGENT
if ($args[0] -eq '--version') { '2.1.0'; exit 0 }
exit 1
`
	paths := map[string]string{}
	for name, file := range map[string]string{"codex": "codex.ps1", "cursor-agent": "cursor-agent.ps1"} {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	look := func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		agent := "cursor"
		if name == paths["codex"] {
			agent = "codex"
		}
		cmd := exec.CommandContext(ctx, "powershell.exe", append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", name}, args...)...)
		cmd.Env = append(os.Environ(), "MEWLA_TEST_COUNT="+countPath, "MEWLA_TEST_AGENT="+agent)
		return cmd.CombinedOutput()
	}
	now := time.Unix(1000, 0)
	options := doctor.Options{ExecutorsPath: configPath, LookPath: look, RunCommand: run}
	probe, unsupported := newHostProbes(func(id string) doctor.ExecutorsCheck {
		options.ExecutorID = id
		return doctor.CheckExecutors(options)
	}, func() time.Time { return now })
	brain.SetHostActiveProbe(probe)
	brain.SetHostUnsupportedProbe(unsupported)
	t.Cleanup(func() {
		brain.SetHostActiveProbe(nil)
		brain.SetHostUnsupportedProbe(nil)
	})
	restoreLook := brain.SetHostLookPath(look)
	t.Cleanup(restoreLook)
	store, err := brain.NewStore(filepath.Join(dir, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	execs, err := work.LoadExecutors(configPath)
	if err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(store, nil, execs)
	refresh := func() {
		t.Helper()
		if _, err := service.ProjectionSnapshot(); err != nil {
			t.Fatal(err)
		}
	}
	counts := func() map[string]int {
		t.Helper()
		out := map[string]int{}
		data, err := os.ReadFile(countPath)
		if os.IsNotExist(err) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			out[strings.TrimSpace(line)]++
		}
		return out
	}
	for _, advance := range []time.Duration{0, 31 * time.Second, 31 * time.Second, 16 * time.Minute} {
		now = now.Add(advance)
		refresh()
	}
	if got := counts(); len(got) != 0 {
		t.Fatalf("Brain refresh launched agents: %v", got)
	}
	// Codex is the deliberate host. Its scoped probe must not launch Cursor.
	probe("codex")
	refresh()
	if got := counts(); got["codex"] == 0 || got["cursor"] != 0 {
		t.Fatalf("selected host probe = %v", got)
	}
	before := counts()["codex"]
	now = now.Add(hostVersionProbeTTL + time.Second)
	probe("codex")
	refresh()
	if got := counts(); got["codex"] != before || got["cursor"] != 0 {
		t.Fatalf("settled cache or snapshot launched an agent: %v", got)
	}
	// Past the settled TTL the probe asks again, still only for Codex (doctor
	// keeps a build's version, so Codex itself need not start again).
	now = now.Add(hostVersionSettledTTL)
	probe("codex")
	if got := counts(); got["cursor"] != 0 {
		t.Fatalf("selected host settled refresh launched Cursor: %v", got)
	}
	t.Logf("Windows PowerShell launches: Brain refresh 0; after selected Codex %v", counts())
}
