package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents"
)

func TestAdapterDeclaresWhatHooksCanAndCannotDo(t *testing.T) {
	d := Adapter{}.Descriptor()
	for capability, want := range map[agentevents.Capability]agentevents.Level{
		agentevents.CapBinding:   agentevents.LevelStructured,
		agentevents.CapEvents:    agentevents.LevelHook,
		agentevents.CapApprove:   agentevents.LevelHook,
		agentevents.CapSend:      agentevents.LevelKeystroke,
		agentevents.CapInterrupt: agentevents.LevelKeystroke,
	} {
		if got := d.Level(capability); got != want {
			t.Fatalf("%s = %s, want %s", capability, got, want)
		}
	}
}

func TestPrepareLaunchAddsTheHookFileOnce(t *testing.T) {
	a := Adapter{SettingsPath: "/state/agent events/claude settings.json"}
	got, err := a.PrepareLaunch("claude --session-id abc")
	if err != nil || got != "claude --session-id abc --settings '/state/agent events/claude settings.json'" {
		t.Fatalf("got %q, %v", got, err)
	}
	own, _ := a.PrepareLaunch("claude --settings /mine.json --session-id abc")
	if own != "claude --settings /mine.json --session-id abc" {
		t.Fatalf("the user's own --settings was changed: %q", own)
	}
	if bare, _ := (Adapter{}).PrepareLaunch("claude --session-id abc"); bare != "claude --session-id abc" {
		t.Fatalf("no settings path, yet %q", bare)
	}
}

func TestPrepareLaunchGivesAFreshLaunchItsOwnSession(t *testing.T) {
	got, err := Adapter{SettingsPath: "/s.json"}.PrepareLaunch("claude --permission-mode auto")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(got)
	if !strings.Contains(got, " --session-id ") || fields[len(fields)-2] != "--settings" {
		t.Fatalf("a fresh launch must carry a session id and the hook file: %q", got)
	}
	resumed, _ := Adapter{SettingsPath: "/s.json"}.PrepareLaunch("claude --resume 11111111-1111-1111-1111-111111111111")
	if strings.Contains(resumed, "--session-id") {
		t.Fatalf("a resumed conversation keeps its own id: %q", resumed)
	}
}

func TestSettingsJSONRegistersEveryHookAndAllowsPermissionToWait(t *testing.T) {
	raw, err := SettingsJSON(`'/bin/mewla' agent-event claude`)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Hooks) != len(HookEvents) {
		t.Fatalf("hooks = %v", parsed.Hooks)
	}
	for _, name := range HookEvents {
		entry := parsed.Hooks[name][0].Hooks[0]
		if entry.Type != "command" || !strings.Contains(entry.Command, "agent-event claude") {
			t.Fatalf("%s = %+v", name, entry)
		}
		if (name == "PermissionRequest") != (entry.Timeout == 600) {
			t.Fatalf("%s timeout = %d", name, entry.Timeout)
		}
	}
}

func TestWriteSettingsIsPrivateAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-events", "claude-settings.json")
	if err := WriteSettings(path, "mewla agent-event claude"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info = %v, err = %v", info, err)
	}
	first, _ := os.ReadFile(path)
	if err := WriteSettings(path, "mewla agent-event claude"); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(path); string(first) != string(second) {
		t.Fatal("rewrite changed the file")
	}
	if err := WriteSettings(path, "other"); err != nil {
		t.Fatal(err)
	}
	if third, _ := os.ReadFile(path); !strings.Contains(string(third), "other") {
		t.Fatal("changed command not written")
	}
}
