//go:build linux

package brain

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

func TestClaudeHostRecoveryAndConcurrentResumeFence(t *testing.T) {
	for _, observed := range []bool{true, false} {
		t.Run(strconv.FormatBool(observed), func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			child := exec.Command("sleep", "60")
			child.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+root)
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
			pid := child.Process.Pid
			stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))[19]
			if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
				t.Fatal(err)
			}
			const token = "5c1666e6-ee0b-4b4e-b708-0a7b4a85ab19"
			raw, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": token, "cwd": store.WorkspacePath(), "procStart": start})
			if err := os.WriteFile(filepath.Join(root, "sessions", strconv.Itoa(pid)+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.ReplaceHostSessionBinding("dead:@2", "claude", token, "/unused/"+token+".jsonl", root); err != nil {
				t.Fatal(err)
			}
			worker := &classifier.Worker{ID: "zen-worker-brain-original:@1649", Name: "Brain", Command: "claude", Cwd: store.WorkspacePath(), Hidden: true, ProcessID: pid}
			fw := &fakeWatcher{sessions: map[string]*classifier.Worker{}}
			if observed {
				fw.workers = []*classifier.Worker{worker}
				fw.sessions[worker.ID] = worker
			}
			service := NewService(store, fw, nil)
			ref, err := service.ensureHostWorker(work.WorkerExecutor{ID: "claude", Provider: "claude", Command: "claude"})
			if observed {
				if err != nil || ref.ID != worker.ID {
					t.Fatalf("ref=%+v err=%v", ref, err)
				}
				host, _ := store.HostSession()
				if host.ID != worker.ID || host.ProviderSessionID != token {
					t.Fatalf("host=%+v", host)
				}
			} else if err == nil || !strings.Contains(err.Error(), "refusing concurrent provider session") {
				t.Fatalf("err=%v", err)
			}
			if len(fw.created) > 0 || len(fw.killed) > 0 {
				t.Fatalf("created=%v killed=%v", fw.created, fw.killed)
			}
		})
	}
}
