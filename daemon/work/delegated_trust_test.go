package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// trustedByLaunch decodes the folder trust a Codex command line carries.
func trustedByLaunch(t *testing.T, command string) map[string]string {
	t.Helper()
	fields, ok := shellquote.Fields(command)
	if !ok {
		t.Fatalf("launch line does not split: %s", command)
	}
	trusted := map[string]string{}
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] != "--config" && fields[index] != "-c" {
			continue
		}
		var cfg struct {
			Projects map[string]struct {
				Trust string `toml:"trust_level"`
			} `toml:"projects"`
		}
		if _, err := toml.Decode(fields[index+1], &cfg); err != nil {
			continue
		}
		for dir, project := range cfg.Projects {
			trusted[dir] = project.Trust
		}
	}
	return trusted
}

func TestDelegatedCodexIsTrustedByItsLaunchAndNothingIsWritten(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		t.Fatal(err)
	}
	original := "# mine\nmodel = 'custom'\n[projects.\"/elsewhere\"]\ntrust_level = 'untrusted'\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"untrusted folder", `it's "odd"; $(x)`} {
		cwd := filepath.Join(root, name)
		if err := os.Mkdir(cwd, 0700); err != nil {
			t.Fatal(err)
		}
		command, err := PrepareDelegatedCommand("codex", "codex")
		if err != nil {
			t.Fatal(err)
		}
		got, err := PrepareDelegatedWorkspace(command, cwd, map[string]string{"HOME": home, "CODEX_HOME": codexHome})
		if err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{CodexFullAuthorizationFlag, "--dangerously-bypass-hook-trust", "check_for_update_on_startup=false"} {
			if !strings.Contains(got, flag) {
				t.Fatalf("missing %s: %s", flag, got)
			}
		}
		real, _ := filepath.EvalSymlinks(cwd)
		if trusted := trustedByLaunch(t, got); trusted[real] != "trusted" || len(trusted) > 2 {
			t.Fatalf("%q: trust = %v, want exactly the cwd", name, trusted)
		}
	}
	after, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if string(after) != original {
		t.Fatalf("the user's Codex config was written:\n%s", after)
	}
	entries, _ := os.ReadDir(codexHome)
	if len(entries) != 1 {
		t.Fatalf("files appeared in CODEX_HOME: %v", entries)
	}
}

func TestDelegatedClaudeTrustIsTheOneKeyInItsOwnConfig(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "untrusted folder")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": root, "CLAUDE_CONFIG_DIR": filepath.Join(root, "claude")}
	command, err := PrepareDelegatedCommand("claude", "claude")
	if err != nil {
		t.Fatal(err)
	}
	got, err := PrepareDelegatedWorkspace(command, cwd, env)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "claude", ".claude.json"))
	var cfg struct {
		Projects map[string]struct {
			Trust bool `json:"hasTrustDialogAccepted"`
		}
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || !cfg.Projects[cwd].Trust {
		t.Fatalf("trust=%s err=%v", raw, err)
	}
	if got != command {
		t.Fatal("Claude execution policy changed")
	}
}

func TestTrustFileFailureDoesNotDamageConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude.json")
	raw := []byte("not valid JSON [[[")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "work")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	command, _ := PrepareDelegatedCommand("claude", "claude")
	if _, err := PrepareDelegatedWorkspace(command, cwd, map[string]string{"HOME": root, "CLAUDE_CONFIG_DIR": root}); err == nil {
		t.Fatal("invalid config accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(raw) {
		t.Fatal("invalid config overwritten")
	}
}

func TestDelegatedCodexTrustFollowsCD(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "actual")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	command := joinLaunchTokens([]string{"codex", "-C", cwd, "--dangerously-bypass-hook-trust"})
	got, err := PrepareDelegatedWorkspace(command, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "--dangerously-bypass-hook-trust") != 1 {
		t.Fatal("duplicated boolean flag")
	}
	real, _ := filepath.EvalSymlinks(cwd)
	if trusted := trustedByLaunch(t, got); trusted[real] != "trusted" || len(trusted) > 2 {
		t.Fatalf("actual cwd not the trusted folder: %v", trusted)
	}
}

func TestDelegatedCursorDisablesUpdaterWithoutChangingTrustPolicy(t *testing.T) {
	command, err := PrepareDelegatedCommand(WorkerProviderCursor, "cursor-agent")
	if err != nil {
		t.Fatal(err)
	}
	got, err := PrepareDelegatedWorkspace(command, "/unused", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--force", "--sandbox disabled", "--trust", "--approve-mcps", "--disable-auto-update"} {
		if !strings.Contains(got, flag) {
			t.Fatalf("missing %s: %s", flag, got)
		}
	}
}

func TestTrustPreparationRetriesConcurrentConfigChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := updateTrustFile(path, func(raw []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return append(raw, "+trusted"...), nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "after+trusted" {
		t.Fatalf("concurrent setting lost: %s", raw)
	}
}
