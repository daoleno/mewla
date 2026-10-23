package server

import (
	"encoding/json"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

func TestWorkerSessionWireNamesTheExecutorAndProvider(t *testing.T) {
	s := &Server{execs: work.NewExecutorConfig(map[string]work.Executor{
		"claude":         {Name: "claude", Command: "claude"},
		"my-codex-proxy": {Name: "my-codex-proxy", Command: "codex-proxy --fast", Kind: "codex"},
	})}
	for _, c := range []struct {
		name         string
		worker       classifier.Worker
		wantExecutor string
		wantProvider string
	}{
		{"recorded at birth", classifier.Worker{ID: "%1", ExecutorID: "my-codex-proxy", Command: "node proxy.js"}, "my-codex-proxy", "codex"},
		{"older pane placed by its program", classifier.Worker{ID: "%2", Command: "claude --permission-mode auto"}, "claude", "claude"},
		{"unclaimed command is a terminal", classifier.Worker{ID: "%3", Command: "zsh"}, "", ""},
	} {
		wire := s.workerSessionWire(&c.worker)
		raw, _ := json.Marshal(wire)
		var got struct {
			ExecutorID string `json:"executor_id"`
			Provider   string `json:"provider"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.ExecutorID != c.wantExecutor || got.Provider != c.wantProvider {
			t.Errorf("%s: executor_id=%q provider=%q, want %q %q (%s)", c.name, got.ExecutorID, got.Provider, c.wantExecutor, c.wantProvider, raw)
		}
	}
}
