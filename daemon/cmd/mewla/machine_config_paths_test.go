package main

import "testing"

// A daemon run beside the person's own leaves their Codex config and Claude
// Code settings alone.
func TestTestDaemonLeavesThePersonsAgentConfigsAlone(t *testing.T) {
	t.Setenv("MEWLA_NO_CONFIG_TAKEOVER", "")
	if codex, claude := machineConfigPaths(); codex == "" || claude == "" {
		t.Fatalf("the person's daemon projects into %q and %q", codex, claude)
	}
	t.Setenv("MEWLA_NO_CONFIG_TAKEOVER", "1")
	if codex, claude := machineConfigPaths(); codex != "" || claude != "" {
		t.Fatalf("a test daemon would write %q and %q", codex, claude)
	}
}
