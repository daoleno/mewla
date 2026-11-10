package work

import (
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/providerpaths"
)

// CodexWorkspaceConversation names the Codex conversation that last ran in
// workspace, a folder only one agent works in (the Brain's): the most
// recently updated interactive thread Codex records there. A Codex TUI keeps
// its conversation in Codex's own app-server daemon, which runs outside the
// TUI's process tree and may have restarted since, so nothing the TUI's
// processes hold open names it.
func CodexWorkspaceConversation(workspace string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	dbPath := providerpaths.CodexStateDB(home)
	if _, err := os.Stat(dbPath); err != nil {
		return "", false
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return "", false
	}
	found, latest := "", time.Time{}
	for _, cwd := range transcriptCWDCandidates(workspace) {
		rows, err := queryCodexThreads(sqlite3, dbPath, cwd)
		if err != nil {
			return "", false
		}
		for _, row := range rows {
			path := strings.TrimSpace(row.RolloutPath)
			if path == "" {
				continue
			}
			meta, err := cachedCodexMeta(path)
			if err != nil || meta.CWD != cwd || strings.EqualFold(meta.Originator, "codex-exec") {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if found == "" || info.ModTime().After(latest) {
				found, latest = strings.TrimSpace(row.ID), info.ModTime()
			}
		}
	}
	return found, found != ""
}
