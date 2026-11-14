package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// A routed Claude launch carries Mewla's hooks in its inline --settings; the
// app (session_created, the Session list, Session details) shows the command
// without them, and the Worker keeps the full one (owner 2026-11-13).
func TestWorkerSessionWireShowsTheLaunchWithoutMewlaHooks(t *testing.T) {
	s := &Server{execs: work.NewExecutorConfig(map[string]work.Executor{"claude": {Name: "claude", Command: "claude"}})}
	full := `claude --model claude-sonnet-5-5 --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:44571/r/rt_x"},"hooks":{"SessionStart":[{"hooks":[{"command":"/bin/mewla agent-event -state-dir /s claude","timeout":10,"type":"command"}]}]},"permissions":{"defaultMode":"bypassPermissions"},"skipDangerousModePermissionPrompt":true}' --session-id 65988fcb-63fc-4e52-a06e-14abf2642c91`
	worker := &classifier.Worker{ID: "mh-1", Command: full}
	raw, err := json.Marshal(s.workerSessionWire(worker))
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Command  string `json:"command"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wire.Command, "agent-event") || strings.Contains(wire.Command, `"hooks"`) {
		t.Fatalf("the shown command carries Mewla's hooks (%d bytes): %s", len(wire.Command), wire.Command)
	}
	if !strings.Contains(wire.Command, "--session-id 65988fcb-63fc-4e52-a06e-14abf2642c91") || !strings.Contains(wire.Command, "ANTHROPIC_BASE_URL") {
		t.Fatalf("the shown command lost the launch: %s", wire.Command)
	}
	if wire.Provider != "claude" {
		t.Fatalf("provider = %q", wire.Provider)
	}
	if worker.Command != full {
		t.Fatal("the Worker's own command was changed")
	}
}
