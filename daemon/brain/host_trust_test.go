package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

// TestMain keeps a Claude Brain Host launched by any test from trusting its
// workspace in the real ~/.claude.json of whoever runs the suite.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "mewla-brain-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func trustedProjects(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	return config.Projects
}

func claudeHostService(t *testing.T) (*Service, *fakeWatcher, *Store) {
	t.Helper()
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "claude")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	}))
	return service, fw, store
}

func TestClaudeHostTrustsItsWorkspaceBeforeLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(config, []byte(`{"hasCompletedOnboarding":true,"theme":"dark","projects":{"/other":{"allowedTools":["x"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service, fw, store := claudeHostService(t)

	if _, err := service.EnsureHostSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 || !strings.HasPrefix(fw.created[0].opts.Command, "claude") {
		t.Fatalf("created host = %#v", fw.created)
	}
	projects := trustedProjects(t, config)
	workspace, err := filepath.EvalSymlinks(store.WorkspacePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{store.WorkspacePath(), workspace} {
		if projects[dir]["hasTrustDialogAccepted"] != true {
			t.Fatalf("workspace %q not trusted: %#v", dir, projects)
		}
	}
	if projects["/other"]["allowedTools"] == nil {
		t.Fatalf("unrelated project lost: %#v", projects)
	}
	raw, _ := os.ReadFile(config)
	if !strings.Contains(string(raw), `"hasCompletedOnboarding": true`) || !strings.Contains(string(raw), `"theme": "dark"`) {
		t.Fatalf("unrelated keys lost: %s", raw)
	}
}

func TestClaudeHostTrustHonorsConfigDirAndWrappedCommand(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	service, fw, store := claudeHostService(t)

	// The real Host command carries an env PATH wrapper and a quoted JSON
	// --settings; the trust step must parse it rather than fail the launch.
	service.trustHostWorkspace(`env PATH=/opt/mewla:$PATH claude --permission-mode bypassPermissions --settings '{"skipDangerousModePermissionPrompt":true}' --add-dir '/w'`, nil)
	if projects := trustedProjects(t, filepath.Join(configDir, ".claude.json")); projects[store.WorkspacePath()]["hasTrustDialogAccepted"] != true {
		t.Fatalf("CLAUDE_CONFIG_DIR not trusted: %#v", projects)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("HOME config written despite CLAUDE_CONFIG_DIR: %v", err)
	}
	if _, err := service.EnsureHostSnapshot(); err != nil || len(fw.created) != 1 {
		t.Fatalf("host launch err=%v created=%#v", err, fw.created)
	}
}

func TestClaudeHostTrustFailureStillLaunchesHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	service, fw, _ := claudeHostService(t)
	if _, err := service.EnsureHostSnapshot(); err != nil || len(fw.created) != 1 {
		t.Fatalf("host launch err=%v created=%#v", err, fw.created)
	}
}

func TestNonClaudeHostLeavesClaudeConfigAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "codex")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Name: "codex", Command: "codex"}}))
	if _, err := service.EnsureHostSnapshot(); err != nil || len(fw.created) != 1 {
		t.Fatalf("host launch err=%v created=%#v", err, fw.created)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("Codex host touched Claude config: %v", err)
	}
	if strings.Contains(fw.created[0].opts.Command, "check_for_update_on_startup") {
		t.Fatalf("Codex host command rewritten: %q", fw.created[0].opts.Command)
	}
}
