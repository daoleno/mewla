package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func claudeRecord(kind, uuid, stamp string, sidechain bool, content ...map[string]any) map[string]any {
	record := map[string]any{
		"type":      kind,
		"sessionId": "main-session",
		"cwd":       "/repo/mewla",
		"uuid":      uuid,
		"timestamp": stamp,
		"message":   map[string]any{"role": kind, "content": content},
	}
	if sidechain {
		record["isSidechain"] = true
		record["agentId"] = "a1"
	}
	return record
}

func writeSubagent(t *testing.T, dir, agentID string, meta map[string]any, records ...any) {
	t.Helper()
	subDir := filepath.Join(dir, "main-session", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(subDir, "agent-"+agentID+".jsonl"), records...)
	raw, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(subDir, "agent-"+agentID+".meta.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMainWithTask(t *testing.T, dir string, taskCallID string, result bool) string {
	t.Helper()
	records := []any{
		claudeRecord("user", "u1", "2026-10-17T10:00:00.000Z", false, map[string]any{"type": "text", "text": "audit sync"}),
		claudeRecord("assistant", "a1", "2026-10-17T10:00:01.000Z", false, map[string]any{
			"type": "tool_use", "id": taskCallID, "name": "Task",
			"input": map[string]any{"description": "Audit the sync module", "subagent_type": "Explore", "prompt": "look"},
		}),
	}
	if result {
		records = append(records, claudeRecord("user", "u2", "2026-10-17T10:02:00.000Z", false, map[string]any{
			"type": "tool_result", "tool_use_id": taskCallID, "content": "Found 3 issues",
		}))
	}
	path := filepath.Join(dir, "main-session.jsonl")
	writeJSONL(t, path, records...)
	return path
}

func TestClaudeSubagentStepsAttachToTheCallThatStartedThem(t *testing.T) {
	dir := t.TempDir()
	path := writeMainWithTask(t, dir, "toolu_task", true)
	writeSubagent(t, dir, "a1", map[string]any{
		"agentType": "Explore", "description": "Audit the sync module", "toolUseId": "toolu_task", "finished": true,
	},
		claudeRecord("assistant", "s1", "2026-10-17T10:00:05.000Z", true, map[string]any{
			"type": "tool_use", "id": "toolu_read", "name": "Read", "input": map[string]any{"file_path": "app/sync.ts"},
		}),
		claudeRecord("user", "s2", "2026-10-17T10:00:06.000Z", true, map[string]any{
			"type": "tool_result", "tool_use_id": "toolu_read", "content": "contents",
		}),
		claudeRecord("assistant", "s3", "2026-10-17T10:00:09.000Z", true, map[string]any{
			"type": "tool_use", "id": "toolu_bash", "name": "Bash", "input": map[string]any{"command": "rg sync app"},
		}),
		claudeRecord("user", "s4", "2026-10-17T10:00:30.000Z", true, map[string]any{
			"type": "tool_result", "tool_use_id": "toolu_bash", "content": "app/sync.ts:1: sync",
		}),
		claudeRecord("assistant", "s5", "2026-10-17T10:01:30.000Z", true, map[string]any{"type": "text", "text": "Found 3 issues"}),
	)

	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, event := range conversation.Events {
		if event.Kind == "assistant_message" && event.Body == "Found 3 issues" {
			t.Fatalf("a sidechain answer leaked into the conversation: %+v", event)
		}
	}
	var task *CodexConversationEvent
	for index := range conversation.Events {
		if conversation.Events[index].CallID == "toolu_task" {
			task = &conversation.Events[index]
		}
	}
	if task == nil || task.Subagent == nil {
		t.Fatalf("the Task call carries no subagent: %+v", task)
	}
	sub := task.Subagent
	if sub.AgentID != "a1" || sub.Type != "Explore" || sub.Description != "Audit the sync module" || !sub.Finished {
		t.Fatalf("subagent identity wrong: %+v", sub)
	}
	if sub.StartedAt == "" || sub.EndedAt == "" || sub.StartedAt >= sub.EndedAt {
		t.Fatalf("subagent span wrong: %q .. %q", sub.StartedAt, sub.EndedAt)
	}
	if len(sub.Steps) != 2 || sub.Steps[0].ToolName != "Read" || sub.Steps[0].Status == "running" ||
		sub.Steps[1].Kind != "command" || sub.Steps[1].Command != "rg sync app" {
		t.Fatalf("steps wrong: %+v", sub.Steps)
	}
}

func TestClaudeSubagentIsRunningUntilItAnswersOrIsMarkedFinished(t *testing.T) {
	dir := t.TempDir()
	path := writeMainWithTask(t, dir, "toolu_task", false)
	writeSubagent(t, dir, "a1", map[string]any{
		"agentType": "general-purpose", "description": "Audit", "toolUseId": "toolu_task",
	},
		claudeRecord("assistant", "s1", "2026-10-17T10:00:05.000Z", true, map[string]any{
			"type": "tool_use", "id": "toolu_read", "name": "Read", "input": map[string]any{"file_path": "a.ts"},
		}),
	)
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	var task CodexConversationEvent
	for _, event := range conversation.Events {
		if event.CallID == "toolu_task" {
			task = event
		}
	}
	if task.Subagent == nil || task.Subagent.Finished || len(task.Subagent.Steps) != 1 || task.Subagent.Steps[0].Status != "running" {
		t.Fatalf("a subagent mid-call is running: %+v", task.Subagent)
	}
}

func TestClaudeSubagentWithoutAMetaFileOrCallIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := writeMainWithTask(t, dir, "toolu_task", true)
	subDir := filepath.Join(dir, "main-session", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No meta file: nothing says which call started it.
	writeJSONL(t, filepath.Join(subDir, "agent-orphan.jsonl"),
		claudeRecord("assistant", "s1", "2026-10-17T10:00:05.000Z", true, map[string]any{"type": "text", "text": "hi"}))
	// A meta file for a call that is not in the conversation window.
	writeSubagent(t, dir, "elsewhere", map[string]any{"toolUseId": "toolu_missing", "description": "x"},
		claudeRecord("assistant", "s2", "2026-10-17T10:00:06.000Z", true, map[string]any{"type": "text", "text": "hi"}))

	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range conversation.Events {
		if event.Subagent != nil {
			t.Fatalf("no subagent should attach: %+v", event)
		}
	}
}

func TestClaudeNestedSubagentAttachesToItsParentsStep(t *testing.T) {
	dir := t.TempDir()
	path := writeMainWithTask(t, dir, "toolu_outer", true)
	writeSubagent(t, dir, "a1", map[string]any{"agentType": "general-purpose", "description": "Outer", "toolUseId": "toolu_outer", "finished": true},
		claudeRecord("assistant", "s1", "2026-10-17T10:00:05.000Z", true, map[string]any{
			"type": "tool_use", "id": "toolu_inner", "name": "Task",
			"input": map[string]any{"description": "Inner", "prompt": "p"},
		}))
	writeSubagent(t, dir, "a2", map[string]any{"agentType": "Explore", "description": "Inner", "toolUseId": "toolu_inner", "finished": true, "parentAgentId": "a1"},
		claudeRecord("assistant", "t1", "2026-10-17T10:00:06.000Z", true, map[string]any{
			"type": "tool_use", "id": "toolu_leaf", "name": "Read", "input": map[string]any{"file_path": "leaf.ts"},
		}))
	conversation, err := parseClaudeConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	var outer CodexConversationEvent
	for _, event := range conversation.Events {
		if event.CallID == "toolu_outer" {
			outer = event
		}
	}
	if outer.Subagent == nil || len(outer.Subagent.Steps) != 1 {
		t.Fatalf("outer: %+v", outer.Subagent)
	}
	inner := outer.Subagent.Steps[0]
	if inner.Subagent == nil || len(inner.Subagent.Steps) != 1 || inner.Subagent.Steps[0].ToolName != "Read" {
		t.Fatalf("inner: %+v", inner.Subagent)
	}
}

func TestClaudeTranscriptStatMovesWhenOnlyASubagentFileDoes(t *testing.T) {
	dir := t.TempDir()
	path := writeMainWithTask(t, dir, "toolu_task", false)
	plain, err := claudeTranscriptStat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, isComposite := plain.(fingerprintedFileInfo); isComposite {
		t.Fatalf("a conversation with no subagents is described by its own file")
	}
	writeSubagent(t, dir, "a1", map[string]any{"toolUseId": "toolu_task", "description": "x"},
		claudeRecord("assistant", "s1", "2026-10-17T10:00:05.000Z", true, map[string]any{"type": "text", "text": "hi"}))
	first, _ := claudeTranscriptStat(path)
	sub := filepath.Join(dir, "main-session", "subagents", "agent-a1.jsonl")
	file, err := os.OpenFile(sub, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("{}\n")
	_ = file.Close()
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(sub, later, later)
	second, _ := claudeTranscriptStat(path)
	if first.Size() == second.Size() && first.ModTime().Equal(second.ModTime()) {
		t.Fatalf("a subagent that grew must change what the cache sees")
	}
	if !sameProviderSourceFile(first, second) {
		t.Fatalf("both describe the same conversation file")
	}
	if !sameProviderSourceFile(plain, first) {
		t.Fatalf("with and without subagents it is still the same file")
	}
}
