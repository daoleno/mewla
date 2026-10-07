//go:build linux

package work

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// claudeBurstSession is one delegated Claude Worker from the 2026-10-07 07:26Z
// burst (mewla-pass-sessions %36, mewla-pass-tools %37, mewla-pass-brand %38):
// one cwd, process starts from /proc starttime, transcript creation from the
// first transcript timestamp.
type claudeBurstSession struct {
	name      string
	sessionID string
	startedAt time.Time
	createdAt time.Time
}

var claudeBurstSessions = []claudeBurstSession{
	{"mewla-pass-sessions", "5034c209-3c47-43ff-9ead-10d63b32d887",
		time.Date(2026, 10, 7, 7, 26, 21, 950_000_000, time.UTC),
		time.Date(2026, 10, 7, 7, 26, 24, 14_000_000, time.UTC)},
	{"mewla-pass-tools", "a1346e2f-3f99-4b7d-8d7d-3cacd969631a",
		time.Date(2026, 10, 7, 7, 26, 23, 810_000_000, time.UTC),
		time.Date(2026, 10, 7, 7, 26, 25, 639_000_000, time.UTC)},
	{"mewla-pass-brand", "69996a29-888c-424d-a820-315691dbff78",
		time.Date(2026, 10, 7, 7, 26, 25, 440_000_000, time.UTC),
		time.Date(2026, 10, 7, 7, 26, 27, 424_000_000, time.UTC)},
}

// startClaudeRegistryProcess starts a stand-in Claude process whose process
// tree exposes configDir and registers it in Claude's process registry
// (<config>/sessions/<pid>.json) exactly as Claude Code does.
func startClaudeRegistryProcess(t *testing.T, configDir, cwd, sessionID string) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	deadline := time.Now().Add(2 * time.Second)
	for firstProcessTreeEnvironValue(pid, "CLAUDE_CONFIG_DIR") != configDir {
		if time.Now().After(deadline) {
			t.Fatal("fixture process did not expose its config directory")
		}
		time.Sleep(10 * time.Millisecond)
	}
	writeClaudeRegistryRecord(t, configDir, pid, cwd, sessionID, procStartForTest(t, pid))
	return pid
}

func writeClaudeRegistryRecord(t *testing.T, configDir string, pid int, cwd, sessionID, procStart string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"pid": pid, "sessionId": sessionID, "cwd": cwd, "procStart": procStart,
		"kind": "interactive", "entrypoint": "cli", "status": "busy",
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(configDir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func procStartForTest(t *testing.T, pid int) string {
	t.Helper()
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))[19]
}

// writeClaudeBurstTranscript writes the transcript's real header shape: the
// leading mode records carry no timestamp, so creation is the first user record.
func writeClaudeBurstTranscript(t *testing.T, configDir, cwd string, session claudeBurstSession, now time.Time) string {
	t.Helper()
	path := filepath.Join(configDir, "projects", encodeClaudeProjectDir(cwd), session.sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, path,
		map[string]any{"type": "mode", "mode": "normal", "sessionId": session.sessionID},
		map[string]any{"type": "permission-mode", "permissionMode": "bypassPermissions", "sessionId": session.sessionID},
		map[string]any{
			"type": "user", "cwd": cwd, "sessionId": session.sessionID, "uuid": session.name + "-u",
			"timestamp": session.createdAt.Format(time.RFC3339Nano),
			"message":   map[string]any{"role": "user", "content": session.name + " delegated prompt"},
		},
	)
	forceReaderFixtureModTime(t, path, now)
	return path
}

// TestProviderConversationReaderClaudeBurstBindsEachProcessOwnTranscript
// reproduces the false turn_lost: Workers launched within ~2s in one cwd made
// the start-time heuristic reject (tools, brand) as near-ties, so the probe
// saw transcript_not_found and the watcher emitted session.uncertain after
// the evidence-loss window. Brand's nearest candidate was the tools
// transcript, so a wider gap would have bound a sibling silently. Claude's
// process registry names each live process's own session.
func TestProviderConversationReaderClaudeBurstBindsEachProcessOwnTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(t.TempDir(), "claude")
	cwd := "/home/daoleno/workspace/zen"
	now := time.Date(2026, 10, 7, 7, 28, 8, 0, time.UTC)

	type bound struct {
		session claudeBurstSession
		pid     int
		path    string
	}
	var workers []bound
	for _, session := range claudeBurstSessions {
		path := writeClaudeBurstTranscript(t, configDir, cwd, session, now)
		pid := startClaudeRegistryProcess(t, configDir, cwd, session.sessionID)
		workers = append(workers, bound{session: session, pid: pid, path: path})
	}

	for _, worker := range workers {
		t.Run(worker.session.name, func(t *testing.T) {
			got, err := NewProviderConversationReader().Load(classifier.Worker{
				Name: "claude", Command: "claude", Cwd: cwd,
				State: classifier.StateRunning, StartedAt: worker.session.startedAt, ProcessID: worker.pid,
			}, WorkerProviderClaude, now)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !got.Available || got.SessionID != worker.session.sessionID || got.Path != worker.path {
				t.Fatalf("%s bound %q (%s, available=%v); want own session %q",
					worker.session.name, got.SessionID, got.Reason, got.Available, worker.session.sessionID)
			}
			if !conversationContainsBody(got, worker.session.name+" delegated prompt") {
				t.Fatalf("events = %#v", got.Events)
			}
		})
	}
}

// A live registry record proves the session even before Claude flushes the
// transcript: the reader must not fall back to a sibling's transcript.
func TestProviderConversationReaderClaudeRegistryNeverBindsSiblingBeforeFirstWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(t.TempDir(), "claude")
	cwd := "/home/daoleno/workspace/zen"
	now := time.Date(2026, 10, 7, 7, 26, 30, 0, time.UTC)
	sibling, own := claudeBurstSessions[0], claudeBurstSessions[1]
	writeClaudeBurstTranscript(t, configDir, cwd, sibling, now)
	pid := startClaudeRegistryProcess(t, configDir, cwd, own.sessionID)

	got, err := NewProviderConversationReader().Load(classifier.Worker{
		Name: "claude", Command: "claude", Cwd: cwd, StartedAt: own.startedAt, ProcessID: pid,
	}, WorkerProviderClaude, now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Available || got.Reason != "transcript_not_found" {
		t.Fatalf("bound a sibling before the owned transcript existed: %#v", got)
	}
}

// A dead Session keeps its stale registry record; a recycled or exited PID
// must not bind through it, so genuine loss still surfaces as unlocatable.
func TestProviderConversationReaderClaudeStaleRegistryDoesNotBind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(t.TempDir(), "claude")
	cwd := "/home/daoleno/workspace/zen"
	now := time.Date(2026, 10, 7, 7, 28, 8, 0, time.UTC)
	tools, brand := claudeBurstSessions[1], claudeBurstSessions[2]
	for _, session := range claudeBurstSessions {
		writeClaudeBurstTranscript(t, configDir, cwd, session, now)
	}
	pid := startClaudeRegistryProcess(t, configDir, cwd, brand.sessionID)
	// The process now at pid is not the one that registered tools' session.
	writeClaudeRegistryRecord(t, configDir, pid, cwd, tools.sessionID, procStartForTest(t, pid)+"0")

	got, err := NewProviderConversationReader().Load(classifier.Worker{
		Name: "claude", Command: "claude", Cwd: cwd, StartedAt: tools.startedAt, ProcessID: pid,
	}, WorkerProviderClaude, now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Available || got.Reason != "transcript_not_found" {
		t.Fatalf("stale registry record bound a transcript: %#v", got)
	}
}
