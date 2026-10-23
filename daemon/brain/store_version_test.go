package brain

import "testing"

func TestStoreVersionAdvancesOnWritesNotReads(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.db.Close() })

	advances := func(name string, write func() error) {
		t.Helper()
		before := store.Version()
		if err := write(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if store.Version() <= before {
			t.Fatalf("%s did not advance the version (%d -> %d)", name, before, store.Version())
		}
	}
	advances("SetChatState", func() error { return store.SetChatState(ChatState{ThreadID: "thread-a"}) })
	advances("SetHostSession", func() error { return store.SetHostSession("%7", "codex") })
	advances("SetHostExecutorID", func() error { return store.SetHostExecutorID("claude") })
	advances("ReplaceHostSessionBinding", func() error {
		return store.ReplaceHostSessionBinding("%8", "codex", "provider-1", "/tmp/t.jsonl", "/tmp")
	})
	advances("SetHostProviderTranscript", func() error {
		return store.SetHostProviderTranscript("provider-2", "/tmp/u.jsonl", "/tmp")
	})

	settled := store.Version()
	if _, err := store.ChatThreadID(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.HostSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ChatThreadIDs(); err != nil {
		t.Fatal(err)
	}
	if got := store.Version(); got != settled {
		t.Fatalf("reads moved the version %d -> %d", settled, got)
	}
}

func TestStoreVersionIsStableAcrossAnUnchangedStore(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.db.Close() })
	first := store.Version()
	for range 3 {
		if got := store.Version(); got != first {
			t.Fatalf("an untouched store reported %d then %d", first, got)
		}
	}
	var nilStore *Store
	if nilStore.Version() != 0 {
		t.Fatal("a nil store has a version")
	}
}
