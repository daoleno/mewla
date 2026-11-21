package work

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/workerproc"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testAntigravityID = "a4fb3316-6bb6-43b9-917e-0431d002503f"

func acpUpdates(t *testing.T, lines ...string) []antigravityACPUpdate {
	t.Helper()
	var out []antigravityACPUpdate
	for _, line := range lines {
		var update antigravityACPUpdate
		if err := json.Unmarshal([]byte(line), &update); err != nil {
			t.Fatalf("bad fixture %s: %v", line, err)
		}
		out = append(out, update)
	}
	return out
}

func TestProjectAntigravityConversation(t *testing.T) {
	updates := acpUpdates(t,
		`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Create hello.txt"}}`,
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Plan "}}`,
		`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"it."}}`,
		`{"sessionUpdate":"tool_call","toolCallId":"c1","name":"write_file","args":{"Path":"/w/hello.txt","Content":"hi\n"},"status":"completed","kind":"edit","title":"File w/hello.txt","locations":[{"path":"/w/hello.txt"}],"rawOutput":{},"content":[{"type":"diff","path":"/w/hello.txt","newText":"hi\n"}]}`,
		`{"sessionUpdate":"tool_call","toolCallId":"c2","name":"run_command","args":{"CommandLine":"false"},"status":"completed","kind":"execute","rawOutput":{"output":"Command exited with code 1.\n\nCommand output:\n"}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done "}}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"now."}}`,
		`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Thanks"}}`,
		`{"sessionUpdate":"usage_update","used":12,"size":100}`,
		`{"sessionUpdate":"some_future_kind","content":{"type":"image"}}`,
	)
	conversation := projectAntigravityConversation(testAntigravityID, "/w", "/db", updates)
	if !conversation.Available || conversation.SessionID != testAntigravityID || conversation.Source != antigravityConversationSource {
		t.Fatalf("conversation = %+v", conversation)
	}
	kinds := []string{}
	for _, event := range conversation.Events {
		kinds = append(kinds, event.Kind)
	}
	want := "user_message reasoning tool_call tool_call assistant_message user_message"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds = %s, want %s", got, want)
	}
	user := conversation.Events[0]
	if user.Body != "Create hello.txt" || user.AdmissionSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("Create hello.txt"))) {
		t.Fatalf("user event = %+v", user)
	}
	if conversation.Events[1].Body != "Plan it." || conversation.Events[4].Body != "Done now." {
		t.Fatalf("chunks were not merged: %q / %q", conversation.Events[1].Body, conversation.Events[4].Body)
	}
	write := conversation.Events[2]
	if write.ToolName != "write_file" || write.Status != "completed" || write.Command != "" || len(write.Files) != 1 || write.Partial {
		t.Fatalf("edit tool = %+v", write)
	}
	run := conversation.Events[3]
	if run.Command != "false" || run.Status != "failed" || run.ExitCode == nil || *run.ExitCode != 1 {
		t.Fatalf("command tool = %+v", run)
	}
	for index, event := range conversation.Events {
		if event.Seq != index+1 || event.ID == "" {
			t.Fatalf("event %d = %+v", index, event)
		}
	}
}

func TestProjectAntigravityToolUpdateMergesIntoOneEvent(t *testing.T) {
	updates := acpUpdates(t,
		`{"sessionUpdate":"tool_call","toolCallId":"c1","name":"run_command","args":{"CommandLine":"ls"},"status":"in_progress","kind":"execute"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed","rawOutput":{"output":"Command exited with code 0.\na"}}`,
	)
	events := projectAntigravityConversation(testAntigravityID, "/w", "/db", updates).Events
	if len(events) != 1 || events[0].Status != "completed" || events[0].Command != "ls" || events[0].Partial || !strings.Contains(events[0].Output, "a") {
		t.Fatalf("events = %+v", events)
	}
}

// fakeAgy writes an `agy` that answers the first request with initialize and
// the second with load, so the replay is tested against a real subprocess.
func fakeAgy(t *testing.T, initReply, loadLines string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = acp ] || exit 2\nread init\n" + initReply + "\nread load\n" + loadLines
	path := filepath.Join(dir, "agy")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellPrint(line string) string { return "printf '%s\\n' '" + line + "'" }

const acpInitOK = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`

func TestReplayAntigravityConversation(t *testing.T) {
	binary := fakeAgy(t, shellPrint(acpInitOK), strings.Join([]string{
		shellPrint(`not json at all`),
		shellPrint(`{"jsonrpc":"2.0","id":77,"method":"fs/read_text_file","params":{}}`),
		shellPrint(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + testAntigravityID + `","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hi"}}}}`),
		shellPrint(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"someone-elses","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"not mine"}}}}`),
		shellPrint(`{"jsonrpc":"2.0","id":2,"result":{}}`),
	}, "\n"))
	updates, err := replayAntigravityConversation(binary, t.TempDir(), testAntigravityID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || antigravityChunkText(updates[0].Content) != "hi" {
		t.Fatalf("updates = %+v", updates)
	}
}

func TestReplayAntigravityConversationDegrades(t *testing.T) {
	cases := []struct {
		name, init, load, want string
	}{
		{"other protocol version", shellPrint(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":2,"agentCapabilities":{"loadSession":true}}}`), "", "protocol version 2"},
		{"no loadSession", shellPrint(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{}}}`), "", "no longer offers session/load"},
		{"not the protocol", shellPrint(`{"jsonrpc":"2.0","id":1,"result":{"hello":"world"}}`), "", "not the Agent Client Protocol"},
		{"initialize refused", shellPrint(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"nope"}}`), "", "refused initialize: nope"},
		{"load fails", shellPrint(acpInitOK), shellPrint(`{"jsonrpc":"2.0","id":2,"error":{"code":-32603,"message":"internal"}}`), "could not load the conversation: internal"},
		{"agy exits", shellPrint(acpInitOK), "exit 0", "stopped before it answered"},
		{"not found is still a failed replay", shellPrint(acpInitOK), shellPrint(`{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"session not found"}}`), "could not load the conversation: session not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			load := tc.load
			if load == "" {
				load = "exit 0"
			}
			_, err := replayAntigravityConversation(fakeAgy(t, tc.init, load), t.TempDir(), testAntigravityID)
			if err == nil {
				t.Fatal("want an error")
			}
			var unreadable *antigravityUnreadableError
			if !errors.As(err, &unreadable) || !strings.Contains(unreadable.detail, tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadAntigravityConversationDegradesWithVisibleReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(AntigravityDataDir(home), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, testAntigravityID+".db")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No agy on PATH at all.
	t.Setenv("PATH", t.TempDir())
	antigravityReplayFailures.mu.Lock()
	antigravityReplayFailures.entries = map[string]antigravityReplayFailure{}
	antigravityReplayFailures.mu.Unlock()
	conversation, err := NewProviderConversationReader().LoadByIdentity(HostTranscriptIdentity{
		Provider: WorkerProviderAntigravity, SessionID: testAntigravityID, Path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Available || !strings.HasPrefix(conversation.Reason, antigravityUnreadableReason+": ") || !strings.Contains(conversation.Reason, "not on PATH") {
		t.Fatalf("conversation = %+v", conversation)
	}
	if conversation.Events == nil || len(conversation.Events) != 0 {
		t.Fatalf("events must be an empty list, got %#v", conversation.Events)
	}
	// The failure is remembered, so a broken agy is not started on every poll.
	if _, failed := recentAntigravityFailure(testAntigravityID, time.Now()); !failed {
		t.Fatal("failure not remembered")
	}
}

func TestLoadAntigravityConversationReadsAndCaches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(AntigravityDataDir(home), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, testAntigravityID+".db")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := fakeAgy(t, shellPrint(acpInitOK), strings.Join([]string{
		"echo run >> \"" + filepath.Join(home, "runs") + "\"",
		shellPrint(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + testAntigravityID + `","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}}}}`),
		shellPrint(`{"jsonrpc":"2.0","id":2,"result":{}}`),
	}, "\n"))
	t.Setenv("PATH", filepath.Dir(binary))
	antigravityReplayFailures.mu.Lock()
	antigravityReplayFailures.entries = map[string]antigravityReplayFailure{}
	antigravityReplayFailures.mu.Unlock()

	reader := NewProviderConversationReader()
	identity := HostTranscriptIdentity{Provider: WorkerProviderAntigravity, SessionID: testAntigravityID, Path: path}
	for i := 0; i < 2; i++ {
		conversation, err := reader.LoadByIdentity(identity)
		if err != nil || !conversation.Available || len(conversation.Events) != 1 || conversation.Events[0].Body != "hello" {
			t.Fatalf("load %d = %+v, %v", i, conversation, err)
		}
	}
	runs, _ := os.ReadFile(filepath.Join(home, "runs"))
	if strings.Count(string(runs), "run") != 1 {
		t.Fatalf("agy replayed %d times for an unchanged conversation", strings.Count(string(runs), "run"))
	}
	// A change to the conversation file replays it.
	later := time.Now().Add(time.Minute)
	if err := os.WriteFile(path, []byte("xy"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(path, later, later)
	if conversation, err := reader.LoadByIdentity(identity); err != nil || !conversation.Available {
		t.Fatalf("load after change = %+v, %v", conversation, err)
	}
	runs, _ = os.ReadFile(filepath.Join(home, "runs"))
	if strings.Count(string(runs), "run") != 2 {
		t.Fatalf("agy replayed %d times, want 2", strings.Count(string(runs), "run"))
	}
}

func writeAntigravitySummary(t *testing.T, home, id, status string, notIdle int) {
	t.Helper()
	dir := AntigravityDataDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS conversation_summaries (conversation_id TEXT PRIMARY KEY, workspace_uris TEXT, last_modified_time TEXT, status TEXT, not_fully_idle INTEGER, killed INTEGER, preview TEXT, parent_conversation_id TEXT)`,
		`DELETE FROM conversation_summaries`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO conversation_summaries VALUES (?, '["file:///w"]', '2026-11-21 20:25:46.5+00:00', ?, ?, 0, '', '')`, id, status, notIdle); err != nil {
		t.Fatal(err)
	}
}

func TestObserveAntigravityConversationStampsWhatItSawAndTracksTheTurn(t *testing.T) {
	home := t.TempDir()
	id := "11111111-2222-4333-8444-555555555555"
	antigravityObservations.mu.Lock()
	antigravityObservations.byConv = map[string]*antigravityObservation{}
	antigravityObservations.mu.Unlock()
	user := func(n int) CodexConversationEvent {
		return CodexConversationEvent{ID: fmt.Sprintf("%s:user:%d", id, n), Kind: "user_message"}
	}
	reply := CodexConversationEvent{ID: id + ":assistant:2", Kind: "assistant_message"}

	// First sight of history, after a daemon start: it carries the file's time.
	writeAntigravitySummary(t, home, id, "CASCADE_RUN_STATUS_IDLE", 0)
	written := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	t0 := time.Date(2026, 11, 21, 20, 30, 0, 0, time.UTC)
	cold := CodexConversation{Events: []CodexConversationEvent{user(1), reply}}
	activity := observeAntigravityConversation(home, id, &cold, written, t0)
	if cold.Events[0].Timestamp != written.Format(time.RFC3339Nano) || cold.Events[1].Timestamp != cold.Events[0].Timestamp {
		t.Fatalf("history stamps = %q %q", cold.Events[0].Timestamp, cold.Events[1].Timestamp)
	}
	if activity == nil || activity.Status != ProviderActivityCompleted || activity.ID != id+":turn:1" || activity.StartedAt != cold.Events[0].Timestamp || activity.SettledAt != activity.StartedAt {
		t.Fatalf("activity = %+v", activity)
	}

	// A second message, seen while agy runs it: stamped when seen, and the turn runs.
	writeAntigravitySummary(t, home, id, "CASCADE_RUN_STATUS_RUNNING", 1)
	t1 := t0.Add(time.Minute)
	warm := CodexConversation{Events: []CodexConversationEvent{user(1), reply, user(2)}}
	activity = observeAntigravityConversation(home, id, &warm, written, t1)
	if warm.Events[0].Timestamp != cold.Events[0].Timestamp || warm.Events[2].Timestamp != t1.Format(time.RFC3339Nano) {
		t.Fatalf("stamps = %q %q", warm.Events[0].Timestamp, warm.Events[2].Timestamp)
	}
	if activity.Status != ProviderActivityRunning || activity.ID != id+":turn:2" || activity.StartedAt != t1.Format(time.RFC3339Nano) || activity.SettledAt != "" {
		t.Fatalf("running activity = %+v", activity)
	}

	// agy settles without writing the conversation: the settle is the time it was seen.
	writeAntigravitySummary(t, home, id, "CASCADE_RUN_STATUS_IDLE", 0)
	t2 := t1.Add(5 * time.Second)
	again := CodexConversation{Events: []CodexConversationEvent{user(1), reply, user(2), {ID: id + ":assistant:4", Kind: "assistant_message"}}}
	activity = observeAntigravityConversation(home, id, &again, written, t2)
	if activity.Status != ProviderActivityCompleted || activity.StartedAt != t1.Format(time.RFC3339Nano) || activity.SettledAt != t2.Format(time.RFC3339Nano) {
		t.Fatalf("settled activity = %+v", activity)
	}
	if again.Events[3].Timestamp != t2.Format(time.RFC3339Nano) {
		t.Fatalf("reply stamp = %q", again.Events[3].Timestamp)
	}
	// Seeing it again does not move the times.
	activity = observeAntigravityConversation(home, id, &again, written, t2.Add(time.Hour))
	if activity.SettledAt != t2.Format(time.RFC3339Nano) {
		t.Fatalf("settle moved: %+v", activity)
	}
	// Background work after a turn keeps it running.
	writeAntigravitySummary(t, home, id, "CASCADE_RUN_STATUS_IDLE", 1)
	if activity = observeAntigravityConversation(home, id, &again, written, t2.Add(2*time.Hour)); activity.Status != ProviderActivityRunning {
		t.Fatalf("not fully idle must be running: %+v", activity)
	}
}

func TestAntigravityConversationFingerprintIgnoresWhatAReplayTouches(t *testing.T) {
	path := filepath.Join(t.TempDir(), testAntigravityID+".db")
	if err := os.WriteFile(path, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := antigravityConversationStat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Replaying opens the database: its -shm and an empty -wal appear or are touched.
	for _, suffix := range []string{"-shm", "-wal"} {
		if err := os.WriteFile(path+suffix, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		later := time.Now().Add(time.Hour)
		_ = os.Chtimes(path+suffix, later, later)
	}
	after, _ := antigravityConversationStat(path)
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("a replay changed the fingerprint: %v/%v -> %v/%v", before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
	// agy appending to the log is a change.
	if err := os.WriteFile(path+"-wal", []byte("frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	if grown, _ := antigravityConversationStat(path); grown.Size() == before.Size() {
		t.Fatal("a growing log must change the fingerprint")
	}
}

func TestParseAntigravityConversationNeverShowsAnEmptyChatForAConversationWithContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	antigravityACPRetryDelay = 0
	defer func() { antigravityACPRetryDelay = 750 * time.Millisecond }()
	writeAntigravitySummary(t, home, testAntigravityID, "CASCADE_RUN_STATUS_IDLE", 0)
	db, err := sql.Open("sqlite", filepath.Join(AntigravityDataDir(home), "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE conversation_summaries SET preview = 'Brain Host Activation Contract'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	path := antigravityConversationPath(home, testAntigravityID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// agy replays updates of a kind this reader has never seen, and no message.
	binary := fakeAgy(t, shellPrint(acpInitOK), strings.Join([]string{
		shellPrint(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + testAntigravityID + `","update":{"sessionUpdate":"turn_v2","content":{"x":1}}}}`),
		shellPrint(`{"jsonrpc":"2.0","id":2,"result":{}}`),
	}, "\n"))
	t.Setenv("PATH", filepath.Dir(binary))
	_, err = parseAntigravityConversation(path)
	var unreadable *antigravityUnreadableError
	if !errors.As(err, &unreadable) || !strings.Contains(unreadable.detail, "none of them is a message Mewla knows") {
		t.Fatalf("err = %v", err)
	}
	// An empty replay of a conversation agy says is empty is just empty.
	writeAntigravitySummary(t, home, testAntigravityID, "CASCADE_RUN_STATUS_IDLE", 0)
	binary = fakeAgy(t, shellPrint(acpInitOK), shellPrint(`{"jsonrpc":"2.0","id":2,"result":{}}`))
	t.Setenv("PATH", filepath.Dir(binary))
	conversation, err := parseAntigravityConversation(path)
	if err != nil || !conversation.Available || len(conversation.Events) != 0 {
		t.Fatalf("empty conversation = %+v, %v", conversation, err)
	}
}

func TestRetryableAntigravityReplay(t *testing.T) {
	for detail, want := range map[string]bool{
		"agy acp stopped before it answered":                       true,
		"agy could not load the conversation: database lock":       true,
		"agy is not on PATH, so its conversation cannot be read":   false,
		"agy acp speaks protocol version 2; Mewla reads version 1": false,
		"this agy no longer offers session/load":                   false,
	} {
		if got := retryableAntigravityReplay(&antigravityUnreadableError{detail: detail}); got != want {
			t.Errorf("%q retryable = %v, want %v", detail, got, want)
		}
	}
	if retryableAntigravityReplay(errors.New("other")) {
		t.Error("an error that is not a replay failure is not retried")
	}
}

// The daemon's StartedAt for a Session is when it last looked at it, so after a
// daemon restart an idle conversation was written before it: found by the
// process's own start time instead.
func TestAntigravityConversationIsFoundByTheProcessStartNotByWhenTheDaemonLooked(t *testing.T) {
	if _, ok := workerproc.StartTime(os.Getpid()); !ok {
		t.Skip("no process start time on this platform")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	writeAntigravitySummary(t, home, testAntigravityID, "CASCADE_RUN_STATUS_IDLE", 0)
	db, err := sql.Open("sqlite", filepath.Join(AntigravityDataDir(home), "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	modified := time.Now().UTC().Format("2006-01-02 15:04:05.999999999+00:00")
	if _, err := db.Exec(`UPDATE conversation_summaries SET workspace_uris = ?, last_modified_time = ?`, `["file://`+cwd+`"]`, modified); err != nil {
		t.Fatal(err)
	}
	db.Close()
	path := antigravityConversationPath(home, testAntigravityID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := fakeAgy(t, shellPrint(acpInitOK), strings.Join([]string{
		shellPrint(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + testAntigravityID + `","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}}}}`),
		shellPrint(`{"jsonrpc":"2.0","id":2,"result":{}}`),
	}, "\n"))
	t.Setenv("PATH", filepath.Dir(binary))
	antigravityReplayFailures.mu.Lock()
	antigravityReplayFailures.entries = map[string]antigravityReplayFailure{}
	antigravityReplayFailures.mu.Unlock()

	rediscovered := time.Now().Add(time.Hour) // the daemon looked at the Session just now
	worker := classifier.Worker{ID: "brain", Cwd: cwd, Command: "agy", StartedAt: rediscovered, ProcessID: os.Getpid()}
	reader := NewProviderConversationReader()
	conversation, err := reader.Load(worker, WorkerProviderAntigravity, time.Now())
	if err != nil || !conversation.Available || len(conversation.Events) != 1 {
		t.Fatalf("with the process's start time: %+v, %v", conversation, err)
	}
	// The same process keeps its conversation: no new lookup, even if the index is gone.
	if err := os.Remove(filepath.Join(AntigravityDataDir(home), "conversation_summaries.db")); err != nil {
		t.Fatal(err)
	}
	if again, err := reader.Load(worker, WorkerProviderAntigravity, time.Now()); err != nil || !again.Available {
		t.Fatalf("pinned: %+v, %v", again, err)
	}
	// A platform that cannot say when the process started falls back to StartedAt.
	worker.ProcessID = 0
	if missing, err := NewProviderConversationReader().Load(worker, WorkerProviderAntigravity, time.Now()); err != nil || missing.Available {
		t.Fatalf("without a process: %+v, %v", missing, err)
	}
}

func TestProjectAntigravitySplitsQueuedWorkEventsBackIntoTheirOwnMessages(t *testing.T) {
	one := "<mewla_work_event>\n{\"event_id\":\"a\"}\n</mewla_work_event>"
	two := "<mewla_work_event>\n{\"event_id\":\"b\"}\n</mewla_work_event>"
	chunk := func(text string) antigravityACPUpdate {
		raw, _ := json.Marshal(map[string]string{"type": "text", "text": text})
		return antigravityACPUpdate{Kind: "user_message_chunk", Content: raw}
	}
	reply := antigravityACPUpdate{Kind: "agent_message_chunk", Content: json.RawMessage(`{"type":"text","text":"ok"}`)}
	events := projectAntigravityConversation(testAntigravityID, "/w", "/db", []antigravityACPUpdate{
		chunk("hello"), reply,
		chunk(one + "\n" + two), reply,
		chunk("prose\n" + one + "\n" + two), // not only Work events: left as agy recorded it
	}).Events
	var users []CodexConversationEvent
	for _, event := range events {
		if event.Kind == "user_message" {
			users = append(users, event)
		}
	}
	if len(users) != 4 || users[1].Body != one || users[2].Body != two || users[1].ID == users[2].ID {
		t.Fatalf("users = %+v", users)
	}
	if users[1].AdmissionSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(one))) || users[2].AdmissionSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(two))) {
		t.Fatalf("each piece is admitted by the digest of what Mewla sent: %+v", users)
	}
	if users[3].Body != "prose\n"+one+"\n"+two {
		t.Fatalf("a message with other text must stay whole: %+v", users)
	}
	for index, event := range events {
		if event.Seq != index+1 {
			t.Fatalf("seq %d = %d", index, event.Seq)
		}
	}
}

func TestApplyAntigravityProgressIsTheTurnNotTheSummaryStatus(t *testing.T) {
	started := time.Date(2026, 11, 21, 21, 33, 0, 0, time.UTC)
	base := func() CodexConversation {
		return CodexConversation{
			Available: true,
			Events:    []CodexConversationEvent{{ID: "u", Kind: "user_message"}, {ID: "a", Kind: "assistant_message"}},
			// what agy's summary row says between tool calls: idle.
			Activity: &ProviderActivity{ID: "c:turn:1", Status: ProviderActivityCompleted, StartedAt: started.Format(time.RFC3339Nano), SettledAt: started.Add(time.Second).Format(time.RFC3339Nano)},
		}
	}
	working := base()
	ApplyAntigravityProgress(&working, "working", started.Add(-time.Minute))
	if working.Activity.Status != ProviderActivityRunning || working.Activity.SettledAt != "" || !working.Events[1].Partial {
		t.Fatalf("working = %+v", working)
	}
	done := base()
	ApplyAntigravityProgress(&done, "done", started.Add(10*time.Second))
	if done.Activity.Status != ProviderActivityCompleted || done.Activity.SettledAt != started.Add(10*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("done = %+v", done.Activity)
	}
	// A turn first seen after it ended cannot have settled before it was seen.
	late := base()
	ApplyAntigravityProgress(&late, "idle", started.Add(-time.Hour))
	if late.Activity.SettledAt != late.Activity.StartedAt {
		t.Fatalf("late = %+v", late.Activity)
	}
	failed := base()
	ApplyAntigravityProgress(&failed, "error", started.Add(time.Second))
	if failed.Activity.Status != ProviderActivityFailed {
		t.Fatalf("failed = %+v", failed.Activity)
	}
	unknown := base()
	ApplyAntigravityProgress(&unknown, "something new", started)
	if unknown.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("an unknown state must change nothing: %+v", unknown.Activity)
	}
	// No turn yet, or a conversation that is not available: nothing to apply to.
	empty := CodexConversation{Available: true}
	ApplyAntigravityProgress(&empty, "working", started)
	if empty.Activity != nil {
		t.Fatal("no activity without a turn")
	}
	// The shared parse is never touched.
	shared := base()
	cached := shared.Events
	ApplyAntigravityProgress(&shared, "working", started)
	if cached[1].Partial {
		t.Fatal("the cached events were changed")
	}
}
