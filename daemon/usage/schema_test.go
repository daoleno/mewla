package usage

import (
	"strings"
	"testing"
)

func TestSchemaAcceptsItsOwnValues(t *testing.T) {
	good := []Event{
		{Name: "install", Props: map[string]string{"os": "linux", "arch": "amd64", "version": "0.5.1", "via": "script"}},
		{Name: "daily", Props: map[string]string{"version": "dev", "signed_in": "true", "devices": "2-3", "agents": "claude,codex", "features": ""}},
		{Name: "first_reply", Props: map[string]string{"after": "5-30m", "agent": "claude"}},
		{Name: "app_daily", Props: map[string]string{"app": strings.Repeat("a", 32), "platform": "ios", "os_major": "18", "version": "0.5.1-beta.2"}},
		{Name: "login_started"},
	}
	for _, e := range good {
		if err := Validate(e); err != nil {
			t.Errorf("%s: %v", e.Name, err)
		}
	}
}

func TestSchemaRefusesAnythingFreeForm(t *testing.T) {
	bad := []Event{
		{Name: "install", Props: map[string]string{"version": "/usr/local/bin/mewla"}},
		{Name: "daily", Props: map[string]string{"agents": "codex,claude"}},       // not sorted
		{Name: "daily", Props: map[string]string{"agents": "claude,claude"}},      // repeated
		{Name: "daily", Props: map[string]string{"features": "terminal,my-repo"}}, // unknown value
		{Name: "login_failed", Props: map[string]string{"reason": "HTTP 500: token abc"}},
		{Name: "error", Props: map[string]string{"component": "brain", "message": "x"}},
		{Name: "app_daily", Props: map[string]string{"app": "alice"}},
		{Name: "prompt", Props: nil},
	}
	for _, e := range bad {
		if err := Validate(e); err == nil {
			t.Errorf("%s %v was accepted", e.Name, e.Props)
		}
	}
}

func TestRefusalNeverEchoesTheValue(t *testing.T) {
	secret := "/home/alice/notes/plan.md"
	err := Validate(Event{Name: "pair_failed", Props: map[string]string{"reason": secret}})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
}

func TestHelpers(t *testing.T) {
	if got := Set([]string{"codex", "claude", "codex", "my-agent"}, AgentKinds); got != "claude,codex" {
		t.Fatalf("Set = %q", got)
	}
	if OneOf("vim", AgentKinds) != "other" || OneOf("claude", AgentKinds) != "claude" {
		t.Fatal("OneOf")
	}
	for n, want := range map[int]string{0: "0", 1: "1", 3: "2-3", 9: "4-9", 40: "10+"} {
		if Bucket(n) != want {
			t.Fatalf("Bucket(%d) = %s", n, Bucket(n))
		}
	}
}
