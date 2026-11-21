package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

func onlyOnThisComputer(programs ...string) func() {
	return work.SetAgentLookPath(func(name string) (string, error) {
		for _, program := range programs {
			if name == program {
				return "/usr/bin/" + name, nil
			}
		}
		return "", os.ErrNotExist
	})
}

// An agent whose program is not on this computer is refused before any Session
// exists, with the cause: nothing is created, and nothing is written for it (no
// folder trust). Before, the Session opened and its terminal said "The program
// ended (exit 127)."
func TestCreateSessionRefusesAMissingAgentBeforeAnythingIsMade(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	defer onlyOnThisComputer("codex")()
	c := newFirstMessageClient(t)
	folder := t.TempDir()
	for _, tc := range []struct{ id, command, text, want string }{
		{"r-claude", "claude --permission-mode bypassPermissions", "", "Claude Code (claude) is not installed on this computer. Install it with: curl -fsSL https://claude.ai/install.sh | bash"},
		// A first message does not change the answer: the agent is the reason.
		{"r-agy", "agy --dangerously-skip-permissions", "Fix it", "Antigravity (agy) is not installed on this computer."},
		{"r-cursor", "cursor-agent --force --sandbox disabled", "", "Cursor Agent (cursor-agent) is not installed on this computer."},
	} {
		frame := map[string]any{"type": "create_session", "request_id": tc.id, "cwd": folder, "command": tc.command}
		if tc.text != "" {
			frame["text"] = tc.text
		}
		c.send(frame)
		reply := c.next(byRequest(tc.id))
		message, _ := reply["message"].(string)
		if reply["type"] != "error" || reply["code"] != "agent_not_installed" || !strings.HasPrefix(message, tc.want) {
			t.Fatalf("%s: reply = %#v", tc.id, reply)
		}
	}
	if workers := c.srv.watcher.Workers(); len(workers) != 0 {
		t.Fatalf("a refused launch made %d Sessions", len(workers))
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
		t.Fatal("a refused launch trusted a folder")
	}
	// The agent that is installed is not refused.
	c.send(map[string]any{"type": "create_session", "request_id": "r-codex", "cwd": folder, "command": "codex"})
	if reply := c.next(byRequest("r-codex")); reply["code"] == "agent_not_installed" {
		t.Fatalf("an installed agent was refused: %#v", reply)
	}
}

// A shell (no command) and a custom command are the person's own: never
// refused for a program that is not an agent's.
func TestCreateSessionNeverRefusesAShellOrACustomCommand(t *testing.T) {
	defer onlyOnThisComputer()()
	c := newFirstMessageClient(t)
	for i, command := range []string{"", "bash", "./run-my-thing --flag", "htop"} {
		id := "r-free-" + string(rune('a'+i))
		c.send(map[string]any{"type": "create_session", "request_id": id, "cwd": t.TempDir(), "command": command})
		if reply := c.next(byRequest(id)); reply["code"] == "agent_not_installed" {
			t.Fatalf("%q was refused as a missing agent: %#v", command, reply)
		}
	}
}
