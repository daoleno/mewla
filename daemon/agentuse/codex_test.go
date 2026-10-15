package agentuse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexsOwnComputerUseIsReadFromItsConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	client := filepath.Join(root, "SkyComputerUseClient")
	if err := os.WriteFile(client, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, config string
		want         bool
	}{
		{"no config", "", false},
		{"installed", "[mcp_servers.computer-use]\ncommand = \"" + client + "\"\nargs = [\"mcp\"]\n", true},
		{"by name on PATH", "[mcp_servers.computer-use]\ncommand = \"cua\"\n", true},
		{"over HTTP", "[mcp_servers.computer-use]\nurl = \"http://127.0.0.1:9/mcp\"\n", true},
		{"turned off", "[mcp_servers.computer-use]\ncommand = \"" + client + "\"\nenabled = false\n", false},
		{"app removed", "[mcp_servers.computer-use]\ncommand = \"/Applications/Gone.app/client\"\n", false},
		{"another server", "[mcp_servers.node_repl]\ncommand = \"node\"\n", false},
		{"unreadable", "[mcp_servers.computer-use\n", false},
	} {
		path := filepath.Join(root, "config.toml")
		os.Remove(path)
		if c.config != "" {
			if err := os.WriteFile(path, []byte(c.config), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := findCodexComputerUse("/nonexistent-home"); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestTheSwitchCoversCodexsOwnComputerUse(t *testing.T) {
	s, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	s.lookChrome = func(string) (chromeInstall, bool) { return chromeInstall{}, false }
	s.lookCodex = func(string) bool { return true }
	if status := s.Refresh(); !status.CodexComputerUse || s.CodexComputerUseOff() {
		t.Fatalf("on: %+v", status)
	}
	if _, err := s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if !s.CodexComputerUseOff() {
		t.Fatal("the switch is off but Codex keeps its own computer use")
	}
}
