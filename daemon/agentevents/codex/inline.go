package codex

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// NoAltScreenFlag runs Codex's TUI inline: its transcript goes into the
// terminal's scrollback instead of an alternate screen, so a viewer that holds
// the scrollback (the phone, a desk terminal) scrolls it on its own, with no
// round trip to the program for each line.
const NoAltScreenFlag = "--no-alt-screen"

// InlineTUI is the command a Session's Codex starts with: inline
// (--no-alt-screen), unless the person chose the screen themselves, in the
// command (--no-alt-screen, a tui.alternate_screen override) or in their
// Codex config ([tui] alternate_screen, in config). Only Codex's TUI is
// touched: the plain interactive launch and `codex resume|fork`; any other
// subcommand, a command with an environment prefix or shell syntax, and
// other programs are returned as they are.
func InlineTUI(command string, config []byte) string {
	fields, ok := shellquote.Fields(strings.TrimSpace(command))
	if !ok || len(fields) == 0 || filepath.Base(fields[0]) != "codex" {
		return command
	}
	if len(fields) > 1 && !strings.HasPrefix(fields[1], "-") && fields[1] != "resume" && fields[1] != "fork" {
		return command
	}
	for i, field := range fields[1:] {
		if field == NoAltScreenFlag {
			return command
		}
		if (field == "-c" || field == "--config") && i+2 < len(fields) && strings.HasPrefix(fields[i+2], "tui.alternate_screen") {
			return command
		}
		if strings.HasPrefix(field, "--config=tui.alternate_screen") || strings.HasPrefix(field, "-ctui.alternate_screen") {
			return command
		}
	}
	if altScreenChosen(config) {
		return command
	}
	return strings.TrimSpace(command) + " " + NoAltScreenFlag
}

// altScreenChosen reports whether the Codex config names the TUI's screen
// ([tui] alternate_screen). A config that cannot be read as TOML is the
// person's own and may say anything, so it counts as a choice.
func altScreenChosen(config []byte) bool {
	if len(strings.TrimSpace(string(config))) == 0 {
		return false
	}
	var parsed struct {
		TUI map[string]any `toml:"tui"`
	}
	if _, err := toml.Decode(string(config), &parsed); err != nil {
		return true
	}
	_, chosen := parsed.TUI["alternate_screen"]
	return chosen
}

// ReadConfig is the Codex config at path (CODEX_HOME's config.toml), or none.
func ReadConfig(path string) []byte {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return raw
}
