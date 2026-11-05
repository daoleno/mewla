package brain

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// switchFixture is a Brain on a grok Host with a visible chat, and codex and
// grok both installed.
func switchFixture(t *testing.T) (*Service, *fakeWatcher, *Store, string) {
	t.Helper()
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "")
	previousLook := hostLookPath
	hostLookPath = func(name string) (string, error) {
		if name == "ghost-agent" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() { hostLookPath = previousLook })
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldHostID := "mewla-worker-brain-old:@1"
	if err := store.SetHostSession(oldHostID, "grok"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-main"}); err != nil {
		t.Fatal(err)
	}
	for index, row := range []TimelineItem{
		{ID: "chat-1", Role: "user", Kind: timelineKindUserMessage, Body: "We are sharing the wechat box with Lin"},
		{ID: "chat-2", Role: "assistant", Kind: timelineKindAssistantMessage, Body: "Sharing it as a single device link."},
	} {
		row.ThreadID, row.SessionID = "thread-main", oldHostID
		row.CreatedAt = time.Now().Add(time.Duration(index) * time.Second)
		if _, err := store.AppendTimelineItem(row); err != nil {
			t.Fatal(err)
		}
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{
		oldHostID: {ID: oldHostID, Name: "Brain", State: classifier.StateRunning, Cwd: store.WorkspacePath(),
			Command: "grok --no-alt-screen --permission-mode bypassPermissions", Hidden: true},
	}}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"grok":  {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions", Kind: "grok", Runtime: work.WorkerRuntimeTmux},
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.WorkerRuntimeTmux},
		"ghost": {Name: "ghost", Command: "ghost-agent", Kind: "grok", Runtime: work.WorkerRuntimeTmux},
	}))
	return service, fw, store, oldHostID
}

// The request path keeps the choice and answers; it neither spawns the new
// Host nor waits for it. The continuity owner brings the Host up afterwards,
// in one delivery that carries the conversation.
func TestSelectHostExecutorAnswersBeforeTheNewHostStarts(t *testing.T) {
	service, fw, store, oldHostID := switchFixture(t)

	snapshot, err := service.SelectHostExecutor("codex")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostExecutor == nil || snapshot.HostExecutor.ID != "codex" {
		t.Fatalf("host executor = %#v", snapshot.HostExecutor)
	}
	if len(fw.created) != 0 || len(fw.killed) != 0 || len(fw.sentCalls) != 0 || fw.readyInputCalls != 0 {
		t.Fatalf("select touched the Host: created=%d killed=%v sent=%d ready=%d", len(fw.created), fw.killed, len(fw.sentCalls), fw.readyInputCalls)
	}
	host, _ := store.HostSession()
	if host.ID != oldHostID || host.ExecutorID != "codex" || store.HostExecutorExplicit() != "codex" {
		t.Fatalf("stored host=%#v explicit=%q", host, store.HostExecutorExplicit())
	}

	if _, err := service.ReconcileHostContinuity(); err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 || len(fw.killed) != 1 || fw.killed[0] != oldHostID {
		t.Fatalf("created=%d killed=%v", len(fw.created), fw.killed)
	}
	if len(fw.sentCalls) != 1 || !strings.Contains(fw.sentCalls[0].text, "user: We are sharing the wechat box with Lin") {
		t.Fatalf("new Host was not given the conversation: %#v", fw.sentCalls)
	}
}

// A choice that cannot work fails at once with its reason and changes nothing.
func TestSelectHostExecutorRejectsWhatCannotRunBeforeChangingAnything(t *testing.T) {
	service, fw, store, oldHostID := switchFixture(t)
	for _, tc := range []struct {
		id      string
		wantErr error
	}{{"ghost", ErrHostNotRunnable}, {"nope", ErrExecutorNotConfigured}, {"", nil}} {
		if _, err := service.SelectHostExecutor(tc.id); err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
			t.Fatalf("select %q: err = %v, want %v", tc.id, err, tc.wantErr)
		}
	}
	t.Setenv("MEWLA_BRAIN_HOST_EXECUTOR", "grok")
	if _, err := service.SelectHostExecutor("codex"); !errors.Is(err, ErrExecutorLockedByEnv) {
		t.Fatalf("locked select: %v", err)
	}
	host, _ := store.HostSession()
	if host.ExecutorID != "grok" || host.ID != oldHostID || len(fw.created) != 0 {
		t.Fatalf("a rejected choice changed Brain: host=%#v created=%d", host, len(fw.created))
	}
}

// The first activation that finds the pane not ready is retried; the retry must
// still carry the conversation, not the bare activation contract.
func TestHostActivationRetryKeepsTheConversation(t *testing.T) {
	service, fw, _, _ := switchFixture(t)
	if _, err := service.SelectHostExecutor("codex"); err != nil {
		t.Fatal(err)
	}
	fw.sendErr = &watcher.InputSubmissionError{
		Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted},
		Cause:  watcher.ErrWorkerInputNotReady,
	}
	if _, err := service.ReconcileHostContinuity(); !errors.Is(err, watcher.ErrWorkerInputNotReady) {
		t.Fatalf("first attempt err = %v", err)
	}
	fw.sendErr = nil
	fw.sentCalls = nil
	if _, err := service.ReconcileHostContinuity(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 || !strings.Contains(fw.sentCalls[0].text, "Sharing it as a single device link.") {
		t.Fatalf("retried activation lost the conversation: %#v", fw.sentCalls)
	}
	// Once accepted it is not repeated.
	fw.sentCalls = nil
	if _, err := service.ReconcileHostContinuity(); err != nil || len(fw.sentCalls) != 0 {
		t.Fatalf("settled activation was resent: err=%v sent=%#v", err, fw.sentCalls)
	}
}

// A switch on a long-running daemon: the old Host is gone and the new one is not
// observed yet. That is "starting", not "did not start"; a launch that really
// fails is named with its reason; a slow-but-fine start never is.
func TestSwitchingHostIsNotReportedAsFailedWhileStarting(t *testing.T) {
	processStartedAt = processStartedAt.Add(-time.Hour)
	t.Cleanup(func() { processStartedAt = processStartedAt.Add(time.Hour) })
	service, fw, _, oldHostID := switchFixture(t)
	delete(fw.sessions, oldHostID)
	fw.workers = nil

	snapshot, err := service.SelectHostExecutor("codex")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostProblem != nil {
		t.Fatalf("a Host that is starting was reported: %#v", snapshot.HostProblem)
	}

	fw.createErr = errors.New("tmux refused the new session")
	if _, err := service.ReconcileHostContinuity(); err == nil {
		t.Fatal("expected the launch failure")
	}
	snapshot, err = service.ProjectionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostProblem == nil || snapshot.HostProblem.Code != "start_failed" || !strings.Contains(snapshot.HostProblem.Detail, "tmux refused the new session") {
		t.Fatalf("a real launch failure was not reported with its reason: %#v", snapshot.HostProblem)
	}

	fw.createErr = nil
	if _, err := service.ReconcileHostContinuity(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.ProjectionSnapshot()
	if err != nil || snapshot.HostProblem != nil {
		t.Fatalf("problem not cleared after the Host started: %#v err=%v", snapshot.HostProblem, err)
	}
}
