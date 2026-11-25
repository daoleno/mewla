package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/usage"
)

func TestTelemetryCommand(t *testing.T) {
	for _, key := range []string{usage.OffEnv, "DO_NOT_TRACK", "CI", usage.EndpointEnv} {
		t.Setenv(key, "")
	}
	usage.Release = true
	t.Cleanup(func() { usage.Release = false })
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		if err := runTelemetryCommand(append(args, "-state-dir", dir), &out, &errOut); err != nil {
			t.Fatalf("%v: %v %s", args, err, errOut.String())
		}
		return out.String()
	}
	if got := run("status"); !strings.Contains(got, "are on") {
		t.Fatalf("status = %q", got)
	}
	usage.Record(dir, "login_started", nil)
	if got := run("show"); !strings.Contains(got, `"login_started"`) {
		t.Fatalf("show = %q", got)
	}
	if got := run("off"); !strings.Contains(got, "off") {
		t.Fatalf("off = %q", got)
	}
	if got := run("show"); !strings.Contains(got, "Nothing waits") {
		t.Fatalf("show after off = %q", got)
	}
	if got := run("status"); !strings.Contains(got, "mewla telemetry on") {
		t.Fatalf("status after off = %q", got)
	}
	run("on")
	before := usage.ReadSettings(dir).InstallID
	run("reset")
	if after := usage.ReadSettings(dir).InstallID; after == before || !usage.ValidID(after) {
		t.Fatalf("reset: %q -> %q", before, after)
	}
	t.Setenv("CI", "true")
	if got := run("status"); !strings.Contains(got, "CI") {
		t.Fatalf("status in CI = %q", got)
	}
}

func TestUsageAgentKindAndReplyFailure(t *testing.T) {
	for id, want := range map[string]string{"claude": "claude", "codex-mini": "codex", "agy": "antigravity", "OpenCode": "opencode", "aider": "other"} {
		if got := usageAgentKind(id); got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
	if usageReplyFailure(agentevents.ErrorAuth) != "provider_auth" || usageReplyFailure(agentevents.ErrorRateLimit) != "quota" || usageReplyFailure(agentevents.ErrorUnknown) != "other" {
		t.Fatal("reply failure classes")
	}
}
