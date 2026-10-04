package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExecutors_Defaults(t *testing.T) {
	cfg, err := LoadExecutors("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	if len(cfg.DeprecatedKeys) != 0 {
		t.Fatalf("deprecated keys = %v", cfg.DeprecatedKeys)
	}
	if _, ok := cfg.ByName["claude"]; !ok {
		t.Fatal("claude missing")
	}
	if worker, ok := cfg.ByName["agent"]; !ok {
		t.Fatal("agent missing")
	} else if worker.Command != "cursor-agent --force --sandbox disabled" || worker.Kind != "cursor" {
		t.Fatalf("agent = %+v", worker)
	}
	if _, ok := cfg.ByName["codex"]; !ok {
		t.Fatal("codex missing")
	}
	if _, ok := cfg.ByName["grok"]; !ok {
		t.Fatal("grok missing")
	}
}

func TestLoadExecutors_CustomFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	err := os.WriteFile(path, []byte(`
delegated_executor = "gpt5"
delegated_model = "gpt-6-astra"
delegated_reasoning = "high"

[[executors]]
name = "claude"
command = "/opt/claude"

[[executors]]
name = "codex"
command = "/opt/codex"

[[executors]]
name = "gpt5"
command = "/opt/gpt5"
`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadExecutors(path)
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	// Retired routing keys still load; they are reported, never applied.
	if strings.Join(cfg.DeprecatedKeys, ",") != "delegated_executor,delegated_model,delegated_reasoning" {
		t.Fatalf("deprecated keys = %v", cfg.DeprecatedKeys)
	}
	if got, err := cfg.ResolveWorkerCommand("codex", "", "", "", true); err != nil || strings.Contains(got, "gpt-6-astra") || strings.Contains(got, "reasoning") {
		t.Fatalf("retired defaults applied: %q, %v", got, err)
	}
	if cfg.ByName["claude"].Command != "/opt/claude" {
		t.Fatalf("claude = %+v", cfg.ByName["claude"])
	}
	if _, ok := cfg.ByName["gpt5"]; !ok {
		t.Fatal("gpt5 missing")
	}
}

func TestLoadExecutors_Roles(t *testing.T) {
	cfg, err := LoadExecutors("/nonexistent")
	if err != nil {
		t.Fatalf("LoadExecutors: %v", err)
	}
	names := cfg.Roles()
	if len(names) < 2 {
		t.Fatalf("roles = %v", names)
	}
}
