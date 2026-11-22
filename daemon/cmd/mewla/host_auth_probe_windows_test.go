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
// script stands in for an installed cursor-agent wrapper without an account.
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
if ($env:MEWLA_TEST_AGENT -eq 'codex' -and $args[0] -eq 'login') { 'Logged in'; exit 0 }
if ($env:MEWLA_TEST_AGENT -eq 'cursor' -and $args[0] -eq 'status') { '{"status":"unauthenticated"}'; exit 0 }
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
	probe, auth, unsupported, account := newHostProbes(func(id string) doctor.ExecutorsCheck {
		options.ExecutorID = id
		return doctor.CheckExecutors(options)
	}, func() time.Time { return now })
	brain.SetHostActiveProbe(probe)
	brain.SetHostUnsupportedProbe(unsupported)
	brain.SetHostAccountProbe(account)
	t.Cleanup(func() {
		brain.SetHostActiveProbe(nil)
		brain.SetHostAuthProbe(nil)
		brain.SetHostUnsupportedProbe(nil)
		brain.SetHostAccountProbe(nil)
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
	// Reproduce the prior all-executor cache through the real Brain refresh.
	// One installed signed-out agent holds it at the 30-second TTL.
	legacyAt := time.Time{}
	legacy := map[string]brain.HostAuthState{}
	brain.SetHostAuthProbe(func(id string) brain.HostAuthState {
		if legacyAt.IsZero() || now.Sub(legacyAt) > hostAuthProbeTTL {
			options.ExecutorID = ""
			legacy = map[string]brain.HostAuthState{}
			for _, item := range doctor.CheckExecutors(options).Items {
				if item.VerifiedAuthenticated {
					legacy[item.ID] = brain.HostAuthAuthenticated
				} else if item.BinaryFound && item.Auth == doctor.AuthUnauthenticated {
					legacy[item.ID] = brain.HostAuthUnauthenticated
				}
			}
			legacyAt = now
		}
		return legacy[id]
	})
	for _, advance := range []time.Duration{0, 31 * time.Second, 31 * time.Second, 16 * time.Minute} {
		now = now.Add(advance)
		refresh()
	}
	red := counts()
	if red["cursor"] < 4 || red["codex"] < 4 {
		t.Fatalf("baseline did not reproduce repeated PowerShell launches: %v", red)
	}
	if err := os.Remove(countPath); err != nil {
		t.Fatal(err)
	}
	brain.SetHostAuthProbe(auth)
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
	now = now.Add(hostAuthProbeTTL + time.Second)
	probe("codex")
	refresh()
	if got := counts(); got["codex"] != before || got["cursor"] != 0 {
		t.Fatalf("settled cache or snapshot launched an agent: %v", got)
	}
	now = now.Add(hostAuthSettledTTL)
	probe("codex")
	if got := counts(); got["codex"] <= before || got["cursor"] != 0 {
		t.Fatalf("selected host settled refresh = %v", got)
	}
	// An explicit diagnostic checks the installed, signed-out Cursor wrapper.
	options.ExecutorID = ""
	full := doctor.CheckExecutors(options)
	if counts()["cursor"] == 0 {
		t.Fatal("explicit diagnosis did not invoke Cursor")
	}
	for _, item := range full.Items {
		if item.ID == "cursor-alias" && item.Auth != doctor.AuthUnauthenticated {
			t.Fatalf("Cursor auth = %s", item.Auth)
		}
	}
	t.Logf("Windows PowerShell launches: baseline Brain refresh %v; fixed Brain refresh 0; after selected Codex %v; after explicit doctor %v", red, before, counts())
}
