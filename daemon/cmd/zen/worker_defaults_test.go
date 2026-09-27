package main

import (
	"bytes"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/work"
	"testing"
)

func TestWorkerDefaultsCLIAndControl(t *testing.T) {
	_, q, err := parseWorkerDefaultsArgs(nil, &bytes.Buffer{})
	if err != nil || q.Type != "worker_defaults_get" {
		t.Fatal(q, err)
	}
	if _, _, err := parseWorkerDefaultsArgs([]string{"-executor", "codex"}, &bytes.Buffer{}); err == nil {
		t.Fatal("partial update accepted")
	}
	_, req, err := parseWorkerDefaultsArgs([]string{"-executor", "codex", "-model", "gpt-6-astra", "-reasoning", "medium"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	a := &controlApp{execs: work.NewExecutorConfig("codex", map[string]work.Executor{"codex": {Name: "codex", Command: "codex --model gpt-6-sol"}})}
	result := a.HandleControlRequest(req)
	if !result.OK || result.WorkerDefaults == nil {
		t.Fatal(result)
	}
	read := a.HandleControlRequest(q)
	if !read.OK || *read.WorkerDefaults != *result.WorkerDefaults {
		t.Fatal(read)
	}
	command, err := a.resolveSpawnCommand(control.Request{Type: "worker_spawn"})
	if err != nil || command != read.WorkerDefaults.Command {
		t.Fatalf("spawn %q %v", command, err)
	}
	hidden, err := a.resolveSpawnCommand(control.Request{Hidden: true})
	if err != nil || hidden != "codex --model gpt-6-sol" {
		t.Fatalf("hidden %q %v", hidden, err)
	}
}
