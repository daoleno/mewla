package main

import (
	"path/filepath"
	"testing"
)

// A daemon run beside the person's own leaves their Codex config and Claude
// Code settings alone.
func TestTestDaemonLeavesThePersonsAgentConfigsAlone(t *testing.T) {
	t.Setenv("MEWLA_NO_CONFIG_TAKEOVER", "")
	if codex, claude := machineConfigPaths(); codex == "" || claude == "" {
		t.Fatalf("the person's daemon projects into %q and %q", codex, claude)
	}
	t.Setenv("MEWLA_NO_CONFIG_TAKEOVER", "1")
	t.Setenv("CODEX_HOME", "")
	if codex, claude := machineConfigPaths(); codex != "" || claude != "" {
		t.Fatalf("a test daemon would write %q and %q", codex, claude)
	}
	own := t.TempDir()
	t.Setenv("CODEX_HOME", own)
	if codex, claude := machineConfigPaths(); codex != filepath.Join(own, "config.toml") || claude != "" {
		t.Fatalf("a test daemon with its own Codex home projects into %q and %q", codex, claude)
	}
}
