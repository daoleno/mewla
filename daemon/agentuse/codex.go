package agentuse

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// CodexComputerUseServer is the MCP server Codex's own computer use adds to
// the user's Codex config on a Mac.
const CodexComputerUseServer = "computer-use"

// findCodexComputerUse says whether the user's Codex config gives Codex its
// own computer use. It reads the config alone; whether the tool works is
// Codex's to say.
func findCodexComputerUse(home string) bool {
	root := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if root == "" {
		if home == "" {
			return false
		}
		root = filepath.Join(home, ".codex")
	}
	var config struct {
		MCPServers map[string]struct {
			Command string `toml:"command"`
			URL     string `toml:"url"`
			Enabled *bool  `toml:"enabled"`
		} `toml:"mcp_servers"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "config.toml"), &config); err != nil {
		return false
	}
	server, ok := config.MCPServers[CodexComputerUseServer]
	if !ok || (server.Enabled != nil && !*server.Enabled) {
		return false
	}
	if filepath.IsAbs(server.Command) {
		_, err := os.Stat(server.Command)
		return err == nil
	}
	return server.Command != "" || server.URL != ""
}

// CodexComputerUseOff says whether a Codex launch must turn off Codex's own
// computer use: the user's Codex has it, and agents may not use this computer.
func (s *Service) CodexComputerUseOff() bool {
	status := s.Status()
	return status.CodexComputerUse && !status.Enabled
}
