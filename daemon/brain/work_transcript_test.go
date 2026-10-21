package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

func TestRecordWorkTranscriptIsAppendOnlyAndDeduplicated(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := WorkTranscriptLink{
		WorkID: "work-1", SessionID: "%12", Executor: "claude",
		ProviderSessionID: "3300a0be-65e7", TranscriptPath: "/home/u/.claude/projects/p/3300a0be-65e7.jsonl",
	}
	if wrote, err := store.RecordWorkTranscript(link); err != nil || !wrote {
		t.Fatalf("first record = %v, %v", wrote, err)
	}
	if wrote, err := store.RecordWorkTranscript(link); err != nil || wrote {
		t.Fatalf("duplicate record = %v, %v", wrote, err)
	}
	// /clear in the same pane starts a new conversation: a second row.
	cleared := link
	cleared.ProviderSessionID, cleared.TranscriptPath = "aaaa1111", "/home/u/.claude/projects/p/aaaa1111.jsonl"
	if wrote, err := store.RecordWorkTranscript(cleared); err != nil || !wrote {
		t.Fatalf("new conversation = %v, %v", wrote, err)
	}
	// A different Work never sees these rows.
	other := link
	other.WorkID = "work-2"
	if _, err := store.RecordWorkTranscript(other); err != nil {
		t.Fatal(err)
	}
	got, err := store.WorkTranscripts("work-1")
	if err != nil || len(got) != 2 || got[0].ProviderSessionID != "3300a0be-65e7" || got[1].ProviderSessionID != "aaaa1111" {
		t.Fatalf("links = %+v err=%v", got, err)
	}
	if got[0].RecordedAt.IsZero() {
		t.Fatal("RecordedAt not set")
	}
	if _, err := store.RecordWorkTranscript(WorkTranscriptLink{WorkID: "w", SessionID: "%1"}); err == nil {
		t.Fatal("link without a conversation accepted")
	}
}

func TestServiceRecordsWorkTranscriptOncePerWorkerInstance(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	identity := work.HostTranscriptIdentity{Provider: "claude", SessionID: "sess-1", Path: "/t/sess-1.jsonl", DataRoot: "/home/u/.claude"}
	service := &Service{store: store, now: time.Now}
	service.resolveWorkerTranscript = func(*classifier.Worker) (string, work.HostTranscriptIdentity, bool, error) {
		calls++
		return "claude", identity, calls > 1, nil // not provable on the first tick
	}
	item := Work{ID: "work-1"}
	worker := &classifier.Worker{ID: "%7", ProcessID: 99, StartedAt: time.Unix(1000, 0)}

	service.recordWorkTranscript(item, worker, "turn-1")
	if got, _ := store.WorkTranscripts("work-1"); len(got) != 0 {
		t.Fatalf("recorded before proof: %+v", got)
	}
	service.recordWorkTranscript(item, worker, "turn-1")
	service.recordWorkTranscript(item, worker, "turn-1")
	got, _ := store.WorkTranscripts("work-1")
	if len(got) != 1 || got[0].Executor != "claude" || got[0].ProviderSessionID != "sess-1" ||
		got[0].TranscriptPath != "/t/sess-1.jsonl" || got[0].TurnID != "turn-1" || got[0].SessionID != "%7" {
		t.Fatalf("links = %+v", got)
	}
	if calls != 2 {
		t.Fatalf("resolver ran %d times; a recorded Worker must not be re-resolved within the recheck window", calls)
	}

	// A resolver error is logged, never fatal.
	service.resolveWorkerTranscript = func(*classifier.Worker) (string, work.HostTranscriptIdentity, bool, error) {
		return "", work.HostTranscriptIdentity{}, false, errors.New("boom")
	}
	service.recordWorkTranscript(item, &classifier.Worker{ID: "%8", ProcessID: 5}, "")
}

func TestHostSessionHistoryKeepsReplacedBindings(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	history := func() []hostSessionHistoryEntry {
		entries, err := store.HostSessionHistory()
		if err != nil {
			t.Fatal(err)
		}
		return entries
	}
	if err := store.ReplaceHostSessionBinding("brain:@1", "claude", "conv-1", "/t/conv-1.jsonl", "/root"); err != nil {
		t.Fatal(err)
	}
	if got := history(); len(got) != 0 {
		t.Fatalf("history before any replacement: %+v", got)
	}
	// New chat replaces the pane and its conversation.
	if err := store.ReplaceHostSessionBinding("brain:@2", "claude", "conv-2", "/t/conv-2.jsonl", "/root"); err != nil {
		t.Fatal(err)
	}
	// Re-binding the same conversation, or clearing the host, logs nothing new.
	if err := store.SetHostProviderTranscript("conv-2", "/t/conv-2.jsonl", "/root"); err != nil {
		t.Fatal(err)
	}
	got := history()
	if len(got) != 1 || got[0].ProviderSessionID != "conv-1" || got[0].TranscriptPath != "/t/conv-1.jsonl" || got[0].ID != "brain:@1" {
		t.Fatalf("history = %+v", got)
	}
	if err := store.SetHostSessionID(""); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceHostSessionBinding("brain:@3", "codex", "conv-3", "/t/conv-3.jsonl", "/root"); err != nil {
		t.Fatal(err)
	}
	got = history()
	if len(got) != 2 || got[1].ProviderSessionID != "conv-2" {
		t.Fatalf("history after clear = %+v", got)
	}
}

func TestHostSessionHistorySeedsCurrentBindingOnOpen(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceHostSessionBinding("brain:@1", "claude", "conv-1", "/t/conv-1.jsonl", "/root"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.HostSessionHistory()
	if err != nil || len(got) != 1 || got[0].ProviderSessionID != "conv-1" {
		t.Fatalf("seed = %+v err=%v", got, err)
	}
	// Replacing it later does not log it twice.
	if err := reopened.ReplaceHostSessionBinding("brain:@2", "claude", "conv-2", "/t/conv-2.jsonl", "/root"); err != nil {
		t.Fatal(err)
	}
	if got, _ := reopened.HostSessionHistory(); len(got) != 1 {
		t.Fatalf("duplicate history entries: %+v", got)
	}
}
