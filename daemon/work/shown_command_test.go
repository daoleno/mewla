package work

import (
	"strings"
	"testing"
)

const hookedClaudeLaunch = `claude --model claude-sonnet-5-5 --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:44571/r/rt_x"},"hooks":{"SessionStart":[{"hooks":[{"command":"/bin/mewla agent-event -state-dir /s claude","timeout":10,"type":"command"}]}],"PermissionRequest":[{"hooks":[{"command":"/bin/mewla agent-event -state-dir /s claude","timeout":600,"type":"command"}]}]},"permissions":{"defaultMode":"bypassPermissions"},"skipDangerousModePermissionPrompt":true}' --session-id 65988fcb-63fc-4e52-a06e-14abf2642c91`

func TestShownCommandLeavesMewlaHooksOut(t *testing.T) {
	got := ShownCommand(hookedClaudeLaunch)
	want := `claude --model claude-sonnet-5-5 --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:44571/r/rt_x"},"permissions":{"defaultMode":"bypassPermissions"},"skipDangerousModePermissionPrompt":true}' --session-id 65988fcb-63fc-4e52-a06e-14abf2642c91`
	if got != want {
		t.Fatalf("shown:\n%s\nwant:\n%s", got, want)
	}
	for _, command := range []string{
		"claude --session-id x",
		`claude --settings '{"hooks":{"Stop":[{"hooks":[{"command":"notify.sh","type":"command"}]}]}}'`,
		"claude --settings /mine.json",
		"codex resume abc",
	} {
		if ShownCommand(command) != command {
			t.Fatalf("%q was changed to %q", command, ShownCommand(command))
		}
	}
	if strings.Contains(ShownCommand(`claude '--settings={"hooks":{"Stop":[{"hooks":[{"command":"m agent-event claude"}]}]}}'`), "agent-event") {
		t.Fatal("--settings= form keeps Mewla's hooks")
	}
}
