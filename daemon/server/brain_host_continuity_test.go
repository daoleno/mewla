package server

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/work"
)

func TestHeartbeatRecoversHostAfterStartupAndBacksOffCrashLoop(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "existing-brain"}); err != nil {
		t.Fatal(err)
	}
	fw := &killTrackingWatcher{}
	service := brain.NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	srv := &Server{brain: service}
	broadcasts := 0
	srv.brainSnapshotBroadcastHook = func(map[string]any) { broadcasts++ }
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	srv.reconcileBrainHostContinuity(now)
	if fw.created != 1 || broadcasts != 1 {
		t.Fatalf("startup: launches=%d broadcasts=%d", fw.created, broadcasts)
	}
	if err := store.SetHostProviderTranscript("exact-active-thread", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, delay := range []time.Duration{20 * time.Second, 40 * time.Second, time.Minute, time.Minute} {
		host, _ := store.HostSession()
		delete(fw.sessions, host.ID)
		created := fw.created
		due := srv.brainHostRetryAt
		srv.reconcileBrainHostContinuity(due.Add(-time.Nanosecond))
		if fw.created != created {
			t.Fatal("retried before backoff elapsed")
		}
		srv.reconcileBrainHostContinuity(due)
		if fw.created != created+1 || srv.brainHostRetryDelay != delay {
			t.Fatalf("crash loop: launches=%d delay=%s", fw.created, srv.brainHostRetryDelay)
		}
		host, _ = store.HostSession()
		if host.ProviderSessionID != "exact-active-thread" {
			t.Fatalf("new native session after crash: %+v", host)
		}
		if token, found, err := work.ProviderResumeToken("codex", fw.sessions[host.ID].Command); err != nil || !found || token != host.ProviderSessionID {
			t.Fatalf("resume token: %s %v", token, err)
		}
	}
	if broadcasts != fw.created {
		t.Fatalf("recovery not broadcast: %d / %d", broadcasts, fw.created)
	}
	stable := srv.brainHostRetryAt
	srv.reconcileBrainHostContinuity(stable)
	srv.reconcileBrainHostContinuity(stable.Add(time.Minute))
	if srv.brainHostRetryDelay != 0 || fw.created != 5 {
		t.Fatalf("healthy host not stable: launches=%d delay=%s", fw.created, srv.brainHostRetryDelay)
	}
	thread, _ := store.ChatThreadID()
	if thread != "existing-brain" {
		t.Fatalf("thread changed: %s", thread)
	}
}
