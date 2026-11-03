package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

func TestAddExecutorKeepsOthersAndConfigDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executors.toml")
	if err := os.WriteFile(path, []byte("[[executors]]\nname = \"claude\"\ncommand = \"claude --model m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := AddExecutor(path, work.Executor{Name: "claude-work", Command: "claude", ConfigDir: "/home/u/.claude_work"}, time.Unix(1, 0))
	if err != nil || backup == "" {
		t.Fatalf("backup=%q err=%v", backup, err)
	}
	cfg, err := work.LoadExecutors(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ByName["claude"].Command != "claude --model m" || cfg.ByName["claude-work"].ConfigDir != "/home/u/.claude_work" {
		t.Fatalf("entries = %#v", cfg.ByName)
	}
	// Adding again replaces, never duplicates.
	if _, err := AddExecutor(path, work.Executor{Name: "claude-work", Command: "claude", ConfigDir: "/home/u/.other"}, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "claude-work") != 1 || !strings.Contains(string(raw), "/home/u/.other") {
		t.Fatalf("file:\n%s", raw)
	}
	// Running setup again must not drop the separate sign-in.
	if _, err := writeExecutorsConfig(configWriteRequest{Path: path, Selected: []selectedExecutor{{ID: "claude-work", Provider: "claude", Existing: work.Executor{Command: "claude"}}}, Now: time.Unix(3, 0)}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = work.LoadExecutors(path)
	if cfg.ByName["claude-work"].ConfigDir != "/home/u/.other" {
		t.Fatalf("setup dropped config_dir: %#v", cfg.ByName["claude-work"])
	}
}
