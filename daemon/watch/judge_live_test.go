package watch

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveAgentsJudgeARealBatch runs each installed agent on the newest real
// signals, in a throwaway store. It costs tokens, so it runs only on request:
// MEWLA_JUDGE_LIVE=claude,codex,pi go test ./watch -run LiveAgents -v
func TestLiveAgentsJudgeARealBatch(t *testing.T) {
	names := os.Getenv("MEWLA_JUDGE_LIVE")
	if names == "" {
		t.Skip("set MEWLA_JUDGE_LIVE to run")
	}
	home, _ := os.UserHomeDir()
	file, err := os.Open(filepath.Join(home, ".mewla", "watch", "signals.jsonl"))
	if err != nil {
		t.Skip(err)
	}
	defer file.Close()
	var signals []Signal
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	for scanner.Scan() {
		var signal Signal
		if json.Unmarshal(scanner.Bytes(), &signal) == nil && signal.Source == SourceHuman {
			signals = append(signals, signal)
		}
	}
	if len(signals) > 25 {
		signals = signals[len(signals)-25:]
	}
	for _, name := range splitNames(names) {
		store, err := OpenStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		store.SetEnabled(true)
		if _, err := store.Append(signals); err != nil {
			t.Fatal(err)
		}
		j := newJudge(store, home)
		j.workdir = t.TempDir()
		j.preferred = func() string { return name }
		j.lookPath = func(candidate string) (string, error) {
			if candidate != name {
				return "", os.ErrNotExist
			}
			return exec.LookPath(candidate)
		}
		runs, err := j.runBatches(context.Background(), 1)
		if err != nil || len(runs) != 1 {
			t.Fatalf("%s: runs=%d err=%v", name, len(runs), err)
		}
		run := runs[0]
		t.Logf("%s: executor=%s model=%s signals=%d placed=%d authors=%d new=%d updated=%d dropped=%d usage=%+v err=%q",
			name, run.Executor, run.Model, run.Signals, run.Placed, run.Authors, run.ProblemsNew, run.ProblemsUpdated, run.Dropped, run.Usage, run.Error)
		if run.Executor != name || run.Error != "" {
			t.Fatalf("%s failed: %+v", name, run)
		}
	}
}

func splitNames(list string) []string {
	var out []string
	for _, name := range strings.Split(list, ",") {
		out = append(out, name)
	}
	return out
}
