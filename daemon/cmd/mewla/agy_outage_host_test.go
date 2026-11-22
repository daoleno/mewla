//go:build ghosttyvt && cgo && linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/sessionhost"
	"github.com/daoleno/mewla/daemon/work"
)

const agyOutageConversation = "22222222-2222-4222-8222-000000000002"

// This runs in the existing daemon-engine CI lane. The provider is the work
// package's local fake ACP; the host, daemon, Worker CLI and relaunch are real.
func TestHostAgyFastOutageHoldsAndResumesOriginalWork(t *testing.T) {
	root, err := os.MkdirTemp("", "ha")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home, stateDir := filepath.Join(root, "home"), filepath.Join(root, "state")
	for _, dir := range []string{home, filepath.Join(home, "run"), stateDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	module := parityModuleRoot(t)
	binary, fake, calls := filepath.Join(root, "mewla"), filepath.Join(root, "fake-agy"), filepath.Join(root, "calls")
	parityBuild(t, module, "./cmd/mewla", binary, nil, nil)
	buildFake := exec.Command("go", "test", "-c", "-o", fake, "./work")
	buildFake.Dir = module
	if output, err := buildFake.CombinedOutput(); err != nil {
		t.Fatalf("build local fake ACP: %v\n%s", err, output)
	}
	wrapper := fmt.Sprintf(`#!/bin/sh
case "$1" in
  acp)
    if [ "${MEWLA_WORKER_DELEGATED:-}" = "1" ]; then
      export MEWLA_FAKE_AGY_CONVERSATION=%s
    else
      unset MEWLA_FAKE_AGY_CONVERSATION
    fi
    exec %q "$@" ;;
  models) printf '%%s\n' 'gemini-4-neon-low' ;;
  *) exit 1 ;;
esac
`, agyOutageConversation, fake)
	if err := os.WriteFile(filepath.Join(root, "agy"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := brain.SaveHostExecutorID(filepath.Join(home, ".mewla", "brain"), "agy"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "run"))
	socket, err := sessionhost.SocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionhost.EnsureSocketDir(socket); err != nil {
		t.Fatal(err)
	}
	hostCtx, stopHost := context.WithCancel(context.Background())
	hostDone := make(chan struct{})
	var hostErr error
	host := sessionhost.NewServer(sessionhost.ServerOptions{SocketPath: socket, HostVersion: "test", Logf: t.Logf})
	go func() { hostErr = host.Run(hostCtx); close(hostDone) }()
	t.Cleanup(func() { stopHost(); <-hostDone })
	waitAgyOutage(t, 10*time.Second, "isolated session host", func() bool {
		select {
		case <-hostDone:
			t.Fatalf("isolated session host %s exited: %v", socket, hostErr)
		default:
		}
		client, err := sessionhost.Dial(context.Background(), socket, "test")
		if err != nil {
			return false
		}
		_ = client.Close()
		return true
	})
	client, err := sessionhost.Dial(context.Background(), socket, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	env := append(parityEnv(home),
		"PATH="+root+":/usr/local/sbin:/usr/local/bin:/usr/bin:/bin",
		"MEWLA_FAKE_AGY_ACP=1", "MEWLA_FAKE_AGY_RECORD="+calls,
	)
	port := parityFreePort(t)
	daemon := &parityDaemon{exited: make(chan struct{}), output: &parityOutput{}, port: port}
	daemon.cmd = exec.Command(binary, "-state-dir", stateDir, "-addr", fmt.Sprintf("127.0.0.1:%d", port), "-loopback-only")
	daemon.cmd.Dir, daemon.cmd.Env = module, env
	daemon.cmd.Stdout, daemon.cmd.Stderr = daemon.output, daemon.output
	if err := daemon.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { daemon.exitErr = daemon.cmd.Wait(); close(daemon.exited) }()
	t.Cleanup(func() { parityStop(t, daemon) })
	if _, err := parityHealth(daemon, 30*time.Second); err != nil {
		t.Fatalf("isolated daemon: %v\n%s", err, daemon.output.String())
	}
	cli := func(args ...string) map[string]json.RawMessage {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir, cmd.Env = module, env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mewla %s: %v\n%s\n%s", strings.Join(args, " "), err, output, daemon.output.String())
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(output, &response); err != nil {
			t.Fatalf("mewla %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		if string(response["ok"]) != "true" {
			t.Fatalf("mewla %s: %s", strings.Join(args, " "), output)
		}
		return response
	}
	spawn := cli("worker", "spawn", "-name", "Agy outage regression", "-executor", "agy",
		"-model", "gemini-4-neon-low", "-cwd", module, "-prompt", "Reply ORIGINAL once",
		"-state-dir", stateDir, "--json")
	var worker struct {
		ID string `json:"id"`
	}
	var item struct {
		WorkID string `json:"work_id"`
	}
	if err := json.Unmarshal(spawn["worker"], &worker); err != nil || worker.ID == "" {
		t.Fatalf("spawn worker: %v %+v", err, spawn)
	}
	if err := json.Unmarshal(spawn["brain_work"], &item); err != nil || item.WorkID == "" {
		t.Fatalf("spawn Work: %v %+v", err, spawn)
	}
	logPath := work.AntigravitySessionLogPath(stateDir, agyOutageConversation)
	waitAgyOutage(t, 30*time.Second, "original fake turn", func() bool {
		return strings.Contains(readAgyOutageFile(logPath), `"stop":"end_turn"`)
	})
	for i := 1; i <= 2; i++ {
		cli("worker", "send", "-id", worker.ID, "-work-id", item.WorkID,
			"-text", fmt.Sprintf("rate limit %d", i), "-state-dir", stateDir, "--json")
		waitAgyOutage(t, 15*time.Second, fmt.Sprintf("429 refusal %d", i), func() bool {
			return strings.Count(readAgyOutageFile(logPath), `"http_status":429`) == i
		})
	}
	waitAgyOutage(t, 10*time.Second, "provider outage", func() bool {
		return strings.Contains(daemon.output.String(), "provider outage: antigravity is refusing requests")
	})
	before, err := client.Info(context.Background(), worker.ID)
	if err != nil || !before.Alive || before.PID <= 0 {
		t.Fatalf("bridge before kill: %+v %v", before, err)
	}
	process, err := os.FindProcess(before.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitAgyOutage(t, 15*time.Second, "outage Waiting", func() bool {
		status := cli("worker", "status", "-id", worker.ID, "-state-dir", stateDir, "--json")
		return strings.Contains(string(status["worker"]), "Waiting for Antigravity")
	})
	// Two supervisor polls pass while the provider is still refusing. No
	// bridge launch or durable restart event is permitted during this hold.
	time.Sleep(35 * time.Second)
	if after, err := client.Info(context.Background(), worker.ID); err != nil || after.Alive {
		t.Fatalf("bridge restarted during outage: %+v %v", after, err)
	}
	if events := agyOutageEvents(t, cli, item.WorkID, stateDir); len(events) != 0 {
		t.Fatalf("outage spent a restart: %+v", events)
	}
	if starts := strings.Count(readAgyOutageFile(calls), agyOutageConversation+" initialize "); starts != 1 {
		t.Fatalf("outage made %d agy starts before recovery", starts)
	}
	brainContext := cli("brain", "context", "-state-dir", stateDir, "--json")
	var contextValue struct {
		HostWorker struct {
			ID string `json:"id"`
		} `json:"host_worker"`
	}
	if err := json.Unmarshal(brainContext["context"], &contextValue); err != nil || contextValue.HostWorker.ID == "" {
		t.Fatalf("host identity: %v %+v", err, brainContext)
	}
	if _, err := client.Input(context.Background(), contextValue.HostWorker.ID,
		[]sessionhost.InputStep{{Paste: "Provider recovered"}, {Key: "Enter"}}); err != nil {
		t.Fatalf("same-provider recovery prompt: %v", err)
	}
	waitAgyOutage(t, 20*time.Second, "same-family answer", func() bool {
		return strings.Contains(daemon.output.String(), "provider outage: antigravity answers again")
	})
	waitAgyOutage(t, 30*time.Second, "one continuation", func() bool {
		events := agyOutageEvents(t, cli, item.WorkID, stateDir)
		return len(events) == 1 && strings.Contains(readAgyOutageFile(logPath), "Your agent process was killed") &&
			strings.Count(readAgyOutageFile(logPath), `"stop":"end_turn"`) == 2
	})
	events := agyOutageEvents(t, cli, item.WorkID, stateDir)
	if len(events) != 1 || events[0].Attempt != 1 || events[0].Conversation != agyOutageConversation || events[0].Outcome != "continued" {
		t.Fatalf("Work relaunch events = %+v", events)
	}
	callLog := readAgyOutageFile(calls)
	if strings.Count(callLog, agyOutageConversation+" session/new ") != 1 ||
		strings.Count(callLog, agyOutageConversation+" session/resume ") != 1 ||
		strings.Count(callLog, agyOutageConversation+" session/prompt ") != 4 ||
		strings.Count(callLog, "Reply ORIGINAL once") != 1 ||
		strings.Count(callLog, "Your agent process was killed") != 1 ||
		!strings.Contains(callLog, `"modelId":"gemini-4-neon-low"`) {
		t.Fatalf("original conversation, prompt or model not preserved:\n%s", callLog)
	}
	finalLog := readAgyOutageFile(logPath)
	if strings.Count(finalLog, "echo: Your agent process was killed") != 1 ||
		strings.Count(finalLog, `"http_status":429`) != 2 {
		t.Fatalf("final fake result or refusals = %s", finalLog)
	}
	t.Logf("Work %s Session %s: two 429s -> Waiting (zero restarts for 35s) -> same-provider answer -> attempt 1 continued in %s; original prompt once, continuation once", item.WorkID, worker.ID, agyOutageConversation)
}

func waitAgyOutage(t *testing.T, timeout time.Duration, event string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", event)
}

func readAgyOutageFile(path string) string {
	raw, _ := os.ReadFile(path)
	return string(raw)
}

func agyOutageEvents(t *testing.T, cli func(...string) map[string]json.RawMessage, workID, stateDir string) []struct {
	Attempt      int    `json:"attempt"`
	Conversation string `json:"conversation"`
	Outcome      string `json:"outcome"`
} {
	t.Helper()
	response := cli("brain", "work", "list", "-id", workID, "-full", "-state-dir", stateDir, "--json")
	var events []struct {
		Kind    string `json:"kind"`
		Details string `json:"details_json"`
	}
	if err := json.Unmarshal(response["brain_work_events"], &events); err != nil {
		t.Fatal(err)
	}
	results := []struct {
		Attempt      int    `json:"attempt"`
		Conversation string `json:"conversation"`
		Outcome      string `json:"outcome"`
	}{}
	for _, event := range events {
		if event.Kind != "session.relaunched" {
			continue
		}
		var details struct {
			Attempt      int    `json:"attempt"`
			Conversation string `json:"conversation"`
			Outcome      string `json:"outcome"`
		}
		if err := json.Unmarshal([]byte(event.Details), &details); err != nil {
			t.Fatal(err)
		}
		results = append(results, details)
	}
	return results
}
