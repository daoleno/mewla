package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDelegatedWorkspaceSeedsNativeTrustBeforeLaunch(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "untrusted folder")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{"HOME": root, "CODEX_HOME": filepath.Join(root, "codex"), "CLAUDE_CONFIG_DIR": filepath.Join(root, "claude")}
			command, err := PrepareDelegatedCommand(provider, provider)
			if err != nil {
				t.Fatal(err)
			}
			got, err := PrepareDelegatedWorkspace(command, cwd, env)
			if err != nil {
				t.Fatal(err)
			}
			if provider == "codex" {
				raw, _ := os.ReadFile(filepath.Join(root, "codex", "config.toml"))
				var cfg struct {
					Projects map[string]struct {
						Trust string `toml:"trust_level"`
					}
				}
				if _, err := toml.Decode(string(raw), &cfg); err != nil || cfg.Projects[cwd].Trust != "trusted" {
					t.Fatalf("trust=%s err=%v", raw, err)
				}
				if !strings.Contains(got, CodexFullAuthorizationFlag) || !strings.Contains(got, "--dangerously-bypass-hook-trust") || !strings.Contains(got, "check_for_update_on_startup=false") {
					t.Fatal(got)
				}
				if len(cfg.Projects) != 1 {
					t.Fatal("ancestor trust was broadened")
				}
			} else {
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
		})
	}
}

func TestCodexTrustPreservesConfigAndIsIdempotent(t *testing.T) {
	for _, raw := range []string{
		"# retained\nmodel = 'custom'\n[features]\nfoo = true\n",
		"# retained\nmodel = 'custom'\n[projects.\"/repo\"]\ntrust_level = 'untrusted'\nkeep = 42\n[features]\nfoo = true\n",
		"# retained\nmodel = 'custom'\n[projects.\"/repo\"]\nkeep = 42\n[features]\nfoo = true\n",
	} {
		got, err := trustCodexDirectory([]byte(raw), "/repo")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "# retained\nmodel = 'custom'") || !strings.Contains(string(got), "[features]\nfoo = true") {
			t.Fatal("unrelated config changed")
		}
		var cfg map[string]any
		if _, err := toml.Decode(string(got), &cfg); err != nil {
			t.Fatal(err)
		}
		again, err := trustCodexDirectory(got, "/repo")
		if err != nil || string(again) != string(got) {
			t.Fatal("trust write not idempotent")
		}
	}
}

func TestTrustFileFailureDoesNotDamageConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	raw := []byte("not valid TOML [[[")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	err := updateTrustFile(path, func(b []byte) ([]byte, error) { return trustCodexDirectory(b, root) })
	if err == nil {
		t.Fatal("invalid config accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(raw) {
		t.Fatal("invalid config overwritten")
	}
}

func TestDelegatedTrustRespectsCommandEnvironmentAndCD(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "actual")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "effective-config")
	command := joinLaunchTokens([]string{"env", "CODEX_HOME=" + config, "codex", "-C", cwd, "--dangerously-bypass-hook-trust"})
	got, err := PrepareDelegatedWorkspace(command, root, map[string]string{"HOME": root})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "--dangerously-bypass-hook-trust") != 1 {
		t.Fatal("duplicated boolean flag")
	}
	raw, err := os.ReadFile(filepath.Join(config, "config.toml"))
	if err != nil || !strings.Contains(string(raw), cwd) {
		t.Fatalf("actual cwd/config not trusted: %v", err)
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
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("model = 'before'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := updateTrustFile(path, func(raw []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			if err := os.WriteFile(path, []byte("model = 'after'\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return trustCodexDirectory(raw, "/repo")
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "model = 'after'") || !strings.Contains(string(raw), "trust_level = \"trusted\"") {
		t.Fatal("concurrent setting lost")
	}
}
