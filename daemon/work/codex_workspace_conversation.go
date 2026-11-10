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
	identity, ok := CodexWorkspaceTranscript(workspace, time.Time{})
	return identity.SessionID, ok
}

// CodexWorkspaceTranscript is the transcript of the most recently updated
// interactive Codex thread in workspace that was written at or after since:
// for a Codex started at since, the conversation it runs.
func CodexWorkspaceTranscript(workspace string, since time.Time) (CodexTranscriptIdentity, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return CodexTranscriptIdentity{}, false
	}
	dbPath := providerpaths.CodexStateDB(home)
	if _, err := os.Stat(dbPath); err != nil {
		return CodexTranscriptIdentity{}, false
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return CodexTranscriptIdentity{}, false
	}
	var found CodexTranscriptIdentity
	var latest time.Time
	for _, cwd := range transcriptCWDCandidates(workspace) {
		rows, err := queryCodexThreads(sqlite3, dbPath, cwd)
		if err != nil {
			return CodexTranscriptIdentity{}, false
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
			if err != nil || info.ModTime().Before(since) {
				continue
			}
			if found.SessionID == "" || info.ModTime().After(latest) {
				found = CodexTranscriptIdentity{SessionID: strings.TrimSpace(row.ID), Path: path, DataRoot: home}
				latest = info.ModTime()
			}
		}
	}
	return found, found.SessionID != ""
}
