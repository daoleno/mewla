package brain

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

type confirmingHostWatcher struct {
	*fakeWatcher
	absent     bool
	confirmErr error
}

func (w *confirmingHostWatcher) ProbeSession(string) (watcher.SessionPresence, error) {
	return watcher.SessionPresenceAbsent, nil
}
func (w *confirmingHostWatcher) ResolveDelegatedAbsence(string) (bool, error) {
	return w.absent, w.confirmErr
}

func TestHostNegativeProbeRequiresReachableConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		confirmErr error
	}{
		{"negative then positive", nil},
		{"negative then transport unknown", errors.New("tmux server unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			id := "mewla-worker-brain-original:@1"
			if err := store.SetHostSession(id, "codex"); err != nil {
				t.Fatal(err)
			}
			fw := &fakeWatcher{sessions: map[string]*classifier.Worker{id: {ID: id, Command: "codex", Hidden: true, Cwd: store.WorkspacePath()}}}
			service := NewService(store, &confirmingHostWatcher{fakeWatcher: fw, confirmErr: tc.confirmErr}, nil)
			_, err = service.ensureHostWorker(work.WorkerExecutor{ID: "codex", Provider: "codex", Command: "codex"})
			if tc.confirmErr != nil && (err == nil || !strings.Contains(err.Error(), "liveness unknown")) {
				t.Fatalf("err=%v", err)
			}
			if tc.confirmErr == nil && err != nil {
				t.Fatal(err)
			}
			host, _ := store.HostSession()
			if host.ID != id || len(fw.created) > 0 || len(fw.killed) > 0 {
				t.Fatalf("host=%+v created=%v killed=%v", host, fw.created, fw.killed)
			}
		})
	}
}

func TestLiveHostWithUnknownIdentityBlocksResume(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceHostSessionBinding("dead:@2", "claude", "provider-session", "/unused/session.jsonl", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	worker := &classifier.Worker{ID: "mewla-worker-brain-alive:@1", Name: "Brain", Command: "claude", Cwd: store.WorkspacePath(), Hidden: true}
	fw := &fakeWatcher{workers: []*classifier.Worker{worker}, sessions: map[string]*classifier.Worker{worker.ID: worker}}
	service := NewService(store, fw, nil)
	_, err = service.ensureHostWorker(work.WorkerExecutor{ID: "claude", Provider: "claude", Command: "claude"})
	if err == nil || !strings.Contains(err.Error(), "identity is unproven") || len(fw.created) > 0 {
		t.Fatalf("err=%v created=%v", err, fw.created)
	}
}

func TestFreshClaudeHostLaunchHasExplicitIdentity(t *testing.T) {
	service := NewService(nil, nil, nil)
	executor := work.WorkerExecutor{ID: "claude", Provider: "claude", Command: "claude"}
	first, err := service.prepareHostLaunch(executor, "", "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.prepareHostLaunch(executor, "", "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	a, present, err := work.ProviderResumeToken("claude", first.command)
	if err != nil || !present || a == "" || !strings.Contains(first.command, "--session-id") {
		t.Fatalf("command=%q token=%q err=%v", first.command, a, err)
	}
	b, _, _ := work.ProviderResumeToken("claude", second.command)
	if a == b {
		t.Fatal("fresh launches reused native identity")
	}
}

func TestConcurrentHostEnsuresLaunchOnlyOnce(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, nil)
	executor := work.WorkerExecutor{ID: "claude", Provider: "claude", Command: "claude"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.ensureHostWorker(executor); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(fw.created) != 1 {
		t.Fatalf("launched %d hosts", len(fw.created))
	}
}
