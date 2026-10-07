//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// Concurrent delegated Claude Workers in one cwd (the 2026-10-07 07:26Z
// mewla-pass burst: real session ids, process starts and transcript creation
// times) must each read as a healthy provider channel. A loss state here is
// what the watcher turns into session.uncertain / turn.lost after the
// evidence-loss window, while the Workers are alive and busy.
func TestWorkProviderActivityProbeClaudeBurstInOneCWDIsHealthy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(t.TempDir(), "claude")
	cwd := "/home/daoleno/workspace/zen"
	projectDir := filepath.Join(configDir, "projects", "-home-daoleno-workspace-zen")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 7, 28, 8, 0, time.UTC)
	sessions := []struct {
		target, sessionID  string
		started, createdAt time.Time
	}{
		{"%36", "5034c209-3c47-43ff-9ead-10d63b32d887",
			time.Date(2026, 10, 7, 7, 26, 21, 950_000_000, time.UTC),
			time.Date(2026, 10, 7, 7, 26, 24, 14_000_000, time.UTC)},
		{"%37", "a1346e2f-3f99-4b7d-8d7d-3cacd969631a",
			time.Date(2026, 10, 7, 7, 26, 23, 810_000_000, time.UTC),
			time.Date(2026, 10, 7, 7, 26, 25, 639_000_000, time.UTC)},
		{"%38", "69996a29-888c-424d-a820-315691dbff78",
			time.Date(2026, 10, 7, 7, 26, 25, 440_000_000, time.UTC),
			time.Date(2026, 10, 7, 7, 26, 27, 424_000_000, time.UTC)},
	}
	probe := newWorkProviderActivityProbe()
	workers := make([]classifier.Worker, 0, len(sessions))
	for _, session := range sessions {
		path := filepath.Join(projectDir, session.sessionID+".jsonl")
		var lines []string
		for _, record := range []map[string]any{
			{"type": "mode", "mode": "normal", "sessionId": session.sessionID},
			{
				"type": "user", "cwd": cwd, "sessionId": session.sessionID, "uuid": session.target + "-u",
				"timestamp": session.createdAt.Format(time.RFC3339Nano),
				"message":   map[string]any{"role": "user", "content": "delegated prompt " + session.target},
			},
		} {
			raw, _ := json.Marshal(record)
			lines = append(lines, string(raw))
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now, now); err != nil {
			t.Fatal(err)
		}
		pid := startClaudeRegistryFixture(t, configDir, cwd, session.sessionID)
		workers = append(workers, classifier.Worker{
			ID: session.target, Name: "claude", Command: "claude", Cwd: cwd,
			State: classifier.StateRunning, StartedAt: session.started, ProcessID: pid, PaneAlive: true,
		})
	}
	for index, worker := range workers {
		observation := probe.ObserveProviderActivity(worker, now)
		if observation.ProbeState != watcher.ProbeStateOK {
			t.Fatalf("%s probe state = %q, want healthy", worker.ID, observation.ProbeState)
		}
		if !strings.Contains(observation.AdmissionStream, "\x00"+sessions[index].sessionID+"\x00") {
			t.Fatalf("%s bound another Session: admission stream %q", worker.ID, observation.AdmissionStream)
		}
	}
}

func startClaudeRegistryFixture(t *testing.T, configDir, cwd, sessionID string) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	deadline := time.Now().Add(2 * time.Second)
	for {
		environ, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
		if strings.Contains(string(environ), "CLAUDE_CONFIG_DIR="+configDir+"\x00") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture process did not expose its config directory")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	procStart := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))[19]
	raw, _ := json.Marshal(map[string]any{
		"pid": pid, "sessionId": sessionID, "cwd": cwd, "procStart": procStart,
	})
	if err := os.MkdirAll(filepath.Join(configDir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "sessions", strconv.Itoa(pid)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return pid
}
