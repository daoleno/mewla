//go:build linux

package work

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/workerproc"
)

func selfReadSyscalls(t *testing.T) uint64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("no /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(line, "syscr: "); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	t.Fatal("no syscr")
	return 0
}

// The watcher asks for the provider activity of every in-flight Worker on each
// poll. A Claude Worker with a dozen tool children must not cost a walk over
// every child's environment each time while its transcript is unchanged.
func TestClaudeLoadOfAnUnchangedTranscriptDoesNotWalkTheProcessTree(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	configDir := filepath.Join(home, ".claude")
	cwd := "/tmp/process-env-cache-cwd"
	projectDir := claudeProjectDir(configDir, cwd)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for i := 0; i < 200; i++ {
		role := []string{"user", "assistant"}[i%2]
		fmt.Fprintf(&lines, `{"type":"%s","uuid":"u%d","timestamp":"2026-11-09T01:%02d:%02d.000Z","cwd":%q,"sessionId":"sess-1","message":{"role":"%s","content":[{"type":"text","text":"line %d"}]}}`+"\n", role, i, (i/60)%60, i%60, cwd, role, i)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "sess-1.jsonl"), []byte(lines.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	pad := strings.Repeat("p", 6000)
	cmd := exec.Command("sh", "-c", "i=0; while [ $i -lt 12 ]; do sleep 120 & i=$((i+1)); done; wait")
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "PAD=" + pad}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	pid := cmd.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for len(workerproc.Descendants(pid)) < 12 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	process, err := workerproc.ReadProcStat(pid)
	if err != nil {
		t.Fatal(err)
	}
	record := fmt.Sprintf(`{"pid":%d,"sessionId":"sess-1","cwd":%q,"procStart":%q}`, pid, cwd, process.Start)
	if err := os.WriteFile(filepath.Join(configDir, "sessions", strconv.Itoa(pid)+".json"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}

	worker := classifier.Worker{ID: "%1", Name: "w", Command: "claude", Cwd: cwd, ProcessID: pid, StartedAt: time.Now().Add(-time.Hour)}
	reader := NewProviderConversationReader()
	now := time.Now()
	conversation, err := reader.Load(worker, WorkerProviderClaude, now)
	if err != nil || !conversation.Available || len(conversation.Events) == 0 {
		t.Fatalf("first load: available=%v events=%d err=%v", conversation.Available, len(conversation.Events), err)
	}
	before := selfReadSyscalls(t)
	for i := 0; i < 5; i++ {
		if _, err := reader.Load(worker, WorkerProviderClaude, now); err != nil {
			t.Fatal(err)
		}
	}
	perLoad := (selfReadSyscalls(t) - before) / 5
	// Uncached, the two environment walks over the 13 processes cost over 100
	// read syscalls per load.
	if perLoad > 40 {
		t.Fatalf("an unchanged load took %d read syscalls", perLoad)
	}
}
