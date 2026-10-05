package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func brainContinuityFixture(t *testing.T) (*Server, *brain.Store, *killTrackingWatcher) {
	t.Helper()
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "existing-brain"}); err != nil {
		t.Fatal(err)
	}
	fw := &killTrackingWatcher{}
	service := brain.NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	return &Server{brain: service, brainHostRecovery: make(chan struct{}, 1)}, store, fw
}

func TestCurrentHostRemovalTriggersExactSessionRecovery(t *testing.T) {
	srv, store, fw := brainContinuityFixture(t)
	srv.reconcileBrainHostContinuity()
	if fw.created != 1 {
		t.Fatalf("startup launches=%d", fw.created)
	}
	if err := store.SetHostProviderTranscript("exact-active-thread", "", ""); err != nil {
		t.Fatal(err)
	}
	host, _ := store.HostSession()
	worker := fw.sessions[host.ID]
	delete(fw.sessions, host.ID)
	removed := watcher.SessionEvent{Type: "worker_removed", WorkerID: host.ID, Worker: worker}
	srv.handleWatcherEvent(removed)
	srv.handleWatcherEvent(removed)
	// Duplicate notifications coalesce; the event broadcaster does not wait
	// for a provider launch or readiness handshake.
	if fw.created != 1 || len(srv.brainHostRecovery) != 1 {
		t.Fatalf("event handler launched or duplicated recovery: %d / %d", fw.created, len(srv.brainHostRecovery))
	}
	<-srv.brainHostRecovery
	srv.reconcileBrainHostContinuity()
	if fw.created != 2 {
		t.Fatalf("loss did not recover: %d", fw.created)
	}
	recovered, _ := store.HostSession()
	if recovered.ProviderSessionID != "exact-active-thread" || recovered.ID == host.ID {
		t.Fatalf("incorrect restored binding: %+v", recovered)
	}
	if token, found, err := work.ProviderResumeToken("codex", fw.sessions[recovered.ID].Command); err != nil || !found || token != recovered.ProviderSessionID {
		t.Fatalf("resume token: %s %v", token, err)
	}
	// A late removal of the departed Host cannot schedule another recovery.
	srv.handleWatcherEvent(removed)
	if len(srv.brainHostRecovery) != 0 || fw.created != 2 {
		t.Fatal("stale removal restarted current Host")
	}
	thread, _ := store.ChatThreadID()
	if thread != "existing-brain" {
		t.Fatalf("thread changed: %s", thread)
	}
}

func TestHostRecoveryIgnoresOutputAndUnrelatedRemoval(t *testing.T) {
	srv, store, fw := brainContinuityFixture(t)
	srv.reconcileBrainHostContinuity()
	host, _ := store.HostSession()
	worker := fw.sessions[host.ID]
	for _, kind := range []string{"worker_output", "worker_state_change", "worker_metadata_change", "worker_discovered", "provider_activity_change"} {
		srv.handleWatcherEvent(watcher.SessionEvent{Type: kind, WorkerID: host.ID, Worker: worker})
	}
	srv.handleWatcherEvent(watcher.SessionEvent{Type: "worker_removed", WorkerID: "other", Worker: &classifier.Worker{ID: "other", Hidden: true}})
	if len(srv.brainHostRecovery) != 0 || fw.created != 1 {
		t.Fatal("ambient events scheduled recovery")
	}
}

func TestHostContinuityStartsAfterDiscoveryAndStopsWithRuntime(t *testing.T) {
	srv, store, fw := brainContinuityFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listed, release := make(chan struct{}), make(chan struct{})
	var firstInventory sync.Once
	w := watcher.New(time.Millisecond)
	w.SetPollSources(watcher.PollSources{
		ListPanes: func() ([]watcher.PollPane, error) {
			firstInventory.Do(func() { close(listed); <-release })
			return nil, nil
		},
		SnapshotProcesses: func() map[int]watcher.PollProcess { return nil },
	})
	srv.watcher = w
	started := make(chan struct{}, 1)
	srv.brainSnapshotBroadcastHook = func(map[string]any) { started <- struct{}{} }
	done, watcherDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(done); srv.runBrainHostContinuity(ctx) }()
	go func() { defer close(watcherDone); _ = w.Run(ctx) }()
	select {
	case <-listed:
	case <-time.After(time.Second):
		t.Fatal("watcher did not begin inventory")
	}
	if fw.created != 0 {
		t.Fatal("started before inventory completed")
	}
	close(release)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("did not restore immediately after discovery")
	}
	if err := store.SetHostProviderTranscript("runtime-native-thread", "", ""); err != nil {
		t.Fatal(err)
	}
	host, _ := store.HostSession()
	worker := fw.sessions[host.ID]
	delete(fw.sessions, host.ID)
	srv.handleWatcherEvent(watcher.SessionEvent{Type: "worker_removed", WorkerID: host.ID, Worker: worker})
	// First the disconnected projection, then the replacement, both emitted
	// without waiting for the unrelated server heartbeat.
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("removal event did not drive recovery")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery owner did not stop")
	}
	select {
	case <-watcherDone:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop")
	}
	if fw.created != 2 {
		t.Fatalf("startup plus removal launches=%d", fw.created)
	}
}
