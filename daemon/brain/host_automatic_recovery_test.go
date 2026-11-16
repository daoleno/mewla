package brain

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

func TestAutomaticHostContinuityCapturesNativeSwitchAndResumesAfterExit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires procfs live rollout evidence")
	}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "current-brain-thread"}); err != nil {
		t.Fatal(err)
	}
	chatBefore, _ := os.ReadFile(store.ChatStatePath())
	const hostID = "mewla-worker-brain:@original"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "sessions", "rollout-active.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	writeCodexRolloutFixture(t, path, "active-native-thread", nil)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cmd := exec.Command("sleep", "60")
	cmd.ExtraFiles = []*os.File{file}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{
		hostID: {ID: hostID, Name: "Brain", Command: "codex", Hidden: true, ProcessID: cmd.Process.Pid, Cwd: store.WorkspacePath()},
	}}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"codex": {Command: `codex --model old-default -c 'model_reasoning_effort="low"'`, Kind: "codex"},
	}))
	// No transcript subscription or user input is needed to persist identity.
	if changed, err := service.ReconcileHostContinuity(); err != nil || changed {
		t.Fatalf("live reconcile: %v %v", changed, err)
	}
	host, _ := store.HostSession()
	if host.ProviderSessionID != "active-native-thread" || host.TranscriptPath != path || host.ProviderDataRoot != root {
		t.Fatalf("unbound live host: %+v", host)
	}
	// Simulate a stale previously saved thread; the live native thread wins.
	if err := store.SetHostProviderTranscript("previous-native-thread", "", root); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcileHostContinuity(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	delete(fw.sessions, hostID)
	if changed, err := service.ReconcileHostContinuity(); err != nil || !changed {
		t.Fatalf("recovery: %v %v", changed, err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("launches: %+v", fw.created)
	}
	launch := fw.created[0].opts.Command
	token, found, err := work.ProviderResumeToken("codex", launch)
	if err != nil || !found || token != "active-native-thread" || strings.Contains(launch, "old-default") || strings.Contains(launch, "model_reasoning_effort") {
		t.Fatalf("wrong recovery command: %q (%v)", launch, err)
	}
	host, _ = store.HostSession()
	if host.ProviderSessionID != token || host.TranscriptPath != path {
		t.Fatalf("lost binding: %+v", host)
	}
	sends := len(fw.sentCalls)
	for i := 0; i < 3; i++ {
		if changed, err := service.ReconcileHostContinuity(); err != nil || changed {
			t.Fatalf("stable reconcile: %v %v", changed, err)
		}
	}
	if len(fw.created) != 1 || len(fw.sentCalls) != sends {
		t.Fatal("stable recovery replayed activation or duplicated host")
	}
	chatAfter, _ := os.ReadFile(store.ChatStatePath())
	if !bytes.Equal(chatBefore, chatAfter) {
		t.Fatal("recovery replaced logical Brain thread")
	}
}

func TestAutomaticHostContinuityFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		session  string
		probeErr error
		hadTurn  bool
	}{
		{"unknown Session liveness", "saved-thread", errors.New("socket unreachable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ReplaceHostSessionBinding("missing:@1", "codex", tc.session, "", ""); err != nil {
				t.Fatal(err)
			}
			if tc.hadTurn {
				item := createSignalTestWork(t, store, "host had a turn", "missing:@1")
				bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
					SessionID: "missing:@1", TurnID: "t1", AcceptedAt: time.Now().UTC(),
				})
				if _, has, err := store.Turn("missing:@1"); err != nil || !has {
					t.Fatalf("fixture turn missing: %v %v", has, err)
				}
			}
			before, _ := os.ReadFile(store.HostSessionPath())
			fw := &fakeWatcher{probeErr: tc.probeErr}
			service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
			if changed, err := service.ReconcileHostContinuity(); err == nil || changed {
				t.Fatalf("unsafe recovery: %v %v", changed, err)
			}
			after, _ := os.ReadFile(store.HostSessionPath())
			if !bytes.Equal(before, after) || len(fw.created) != 0 || len(fw.killed) != 0 || len(fw.sentCalls) != 0 {
				t.Fatal("uncertain recovery mutated host")
			}
		})
	}
}

// A Brain recorded from the tmux era ("%0"), on codex, with a turn admitted and
// no native conversation recorded: its Session is gone. It used to be refused
// on every reconcile forever. Now a fresh Brain starts with the handoff, Chat
// says once that the earlier conversation was not resumed, and codex's login
// status (signed out: it may run on an API key) keeps nothing from launching.
func TestAutomaticHostContinuityStartsFreshForALegacyHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceHostSessionBinding("%0", "codex", "", "", ""); err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "host had a turn", "%0")
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{SessionID: "%0", TurnID: "t1", AcceptedAt: time.Now().UTC()})
	previous := hostLookPath
	hostLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { hostLookPath = previous })
	SetHostAuthProbe(func(string) HostAuthState { return HostAuthUnauthenticated })
	t.Cleanup(func() { SetHostAuthProbe(nil) })
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	if changed, err := service.ReconcileHostContinuity(); err != nil || !changed {
		t.Fatalf("legacy Host not replaced: %v %v", changed, err)
	}
	if len(fw.created) != 1 || strings.Contains(fw.created[0].opts.Command, "resume") {
		t.Fatalf("created %#v: want one fresh Brain", fw.created)
	}
	if len(fw.sentCalls) != 1 || !strings.Contains(fw.sentCalls[0].text, "Recover active work from current.md") {
		t.Fatalf("fresh Brain was not handed the workspace: %#v", fw.sentCalls)
	}
	host, _ := store.HostSession()
	if host.ID == "%0" || host.ID != fw.created[0].id && !strings.HasPrefix(host.ID, "mewla-worker-brain") {
		t.Fatalf("host = %+v", host)
	}
	for i := 0; i < 3; i++ {
		if changed, err := service.ReconcileHostContinuity(); err != nil || changed {
			t.Fatalf("stable reconcile %d: %v %v", i, changed, err)
		}
	}
	if len(fw.created) != 1 {
		t.Fatalf("duplicate Brain: %#v", fw.created)
	}
	assertHostRestartNotice(t, store, host.ID)
	if problem := service.projectHostProblem(WorkerRef{}, service.hostExecutor()); problem != nil && problem.Code == "not_signed_in" {
		t.Fatalf("no-host problem trusts the login status: %+v", problem)
	}
}

// A Host that opened while its agent was signed out sits at the agent's own
// sign-in screen, which never notices a sign-in made in another terminal. Once
// the agent is no longer signed out the Host opens again by itself; while it
// stays signed out it is left alone.
func TestHostAtSignInScreenOpensAgainAfterSignIn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostExecutorID("codex"); err != nil {
		t.Fatal(err)
	}
	previous := hostLookPath
	hostLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { hostLookPath = previous })
	auth := HostAuthUnauthenticated
	SetHostAuthProbe(func(string) HostAuthState { return auth })
	t.Cleanup(func() { SetHostAuthProbe(nil) })
	fw := &fakeWatcher{sendErr: watcher.ErrWorkerInputNotReady}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	if _, err := service.ReconcileHostContinuity(); !errors.Is(err, watcher.ErrWorkerInputNotReady) {
		t.Fatalf("first launch: %v", err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created %#v", fw.created)
	}
	first := fw.created[0].id
	for i := 0; i < 2; i++ {
		_, _ = service.ReconcileHostContinuity()
	}
	if len(fw.created) != 1 || len(fw.killed) != 0 {
		t.Fatalf("still signed out, yet reopened: created %#v killed %#v", fw.created, fw.killed)
	}
	auth = HostAuthAuthenticated
	if changed, _ := service.ReconcileHostContinuity(); !changed {
		t.Fatal("signed in, yet the Host at the sign-in screen stayed")
	}
	if len(fw.created) != 2 || len(fw.killed) != 1 || fw.killed[0] != first {
		t.Fatalf("created %#v killed %#v: want the sign-in screen closed and one new Brain", fw.created, fw.killed)
	}
	if host, _ := store.HostSession(); host.ID != fw.created[1].id || host.ExecutorID != "codex" {
		t.Fatalf("host after reopening = %+v", host)
	}
	if store.HostExecutorExplicit() != "codex" {
		t.Fatal("reopening dropped the person's choice of agent")
	}
	if items, _ := store.ThreadTimeline(mustChatThread(t, store), defaultThreadTimelineLimit); len(items) != 0 {
		t.Fatalf("a Host that held no conversation wrote %+v", items)
	}
	// The new Host opened signed in: it is never reopened for sign-in again,
	// however long it takes to become ready.
	_, _ = service.ReconcileHostContinuity()
	fw.sendErr = nil
	if _, err := service.ReconcileHostContinuity(); err != nil || len(fw.created) != 2 || len(fw.killed) != 1 {
		t.Fatalf("stable after reopening: %v created %d killed %d", err, len(fw.created), len(fw.killed))
	}
}

// An update or restart adopts a Host this process did not open: how it
// opened is unknown. One that sits unready past the grace while its agent is
// not signed out (the person signed in before updating) is opened again once;
// it never churns after that, and a Host holding a conversation is never closed.
func TestAdoptedHostAtSignInScreenOpensAgainOnce(t *testing.T) {
	for _, held := range []bool{false, true} {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetHostExecutorID("codex"); err != nil {
			t.Fatal(err)
		}
		const adopted = "mh-adopted"
		if err := store.SetHostSession(adopted, "codex"); err != nil {
			t.Fatal(err)
		}
		if held {
			if err := store.SetHostProviderTranscript("thread-1", "", ""); err != nil {
				t.Fatal(err)
			}
		}
		previous := hostLookPath
		hostLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
		SetHostAuthProbe(func(string) HostAuthState { return HostAuthAuthenticated })
		fw := &fakeWatcher{
			sendErr:  watcher.ErrWorkerInputNotReady,
			sessions: map[string]*classifier.Worker{adopted: {ID: adopted, Name: "Brain", Hidden: true, Command: "codex", Cwd: store.WorkspacePath()}},
		}
		service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
		_, _ = service.ReconcileHostContinuity()
		if len(fw.killed) != 0 || len(fw.created) != 0 {
			t.Fatalf("held=%v: reopened within the grace: killed %#v created %#v", held, fw.killed, fw.created)
		}
		pastNotReadyGrace(service)
		for i := 0; i < 3; i++ {
			_, _ = service.ReconcileHostContinuity()
		}
		switch {
		case held && (len(fw.killed) != 0 || len(fw.created) != 0):
			t.Fatalf("a Host holding a conversation was closed: killed %#v created %#v", fw.killed, fw.created)
		case !held && (len(fw.killed) != 1 || fw.killed[0] != adopted || len(fw.created) != 1):
			t.Fatalf("killed %#v created %#v: want the adopted sign-in screen closed and one new Brain", fw.killed, fw.created)
		}
		hostLookPath = previous
		SetHostAuthProbe(nil)
	}
}

// exitedSessionWatcher is a watcher whose session host keeps a Session whose
// program ended.
type exitedSessionWatcher struct {
	*fakeWatcher
	exited string
}

func (w exitedSessionWatcher) SessionExited(id string) bool {
	_, present := w.sessions[id]
	return id == w.exited && present
}

// The session host keeps a Session whose program ended, where a closed
// pane. A Brain whose agent ended is gone as if its pane had closed: the ended
// Session is closed and the Brain starts again in its conversation.
func TestAutomaticHostContinuityResumesABrainWhoseProgramEnded(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const conversation = "11111111-2222-4333-8444-555555555555"
	if err := store.ReplaceHostSessionBinding("mh-brain", "codex", conversation, "", ""); err != nil {
		t.Fatal(err)
	}
	previous := hostLookPath
	hostLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { hostLookPath = previous })
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{"mh-brain": {ID: "mh-brain", Name: "Brain", Command: "codex"}}}
	service := NewService(store, exitedSessionWatcher{fakeWatcher: fw, exited: "mh-brain"}, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	// Nothing reports the Session removed; the recovery tick finds it.
	if !service.HostRecoveryDue() {
		t.Fatal("a Brain whose program ended is not due for recovery")
	}
	if changed, err := service.ReconcileHostContinuity(); err != nil || !changed {
		t.Fatalf("restart the ended Brain: %v %v", changed, err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != "mh-brain" {
		t.Fatalf("killed %#v, want the ended Session closed", fw.killed)
	}
	if len(fw.created) != 1 || !strings.Contains(fw.created[0].opts.Command, "resume "+conversation) {
		t.Fatalf("created %#v, want the Brain resumed in its conversation", fw.created)
	}
}

// A computer rebooted before its first message has a recorded host that never
// held a conversation: nothing to lose, so Brain starts again by itself.
func TestAutomaticHostContinuityRestartsAHostThatNeverHeldAConversation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceHostSessionBinding("missing:@1", "codex", "", "", ""); err != nil {
		t.Fatal(err)
	}
	previous := hostLookPath
	hostLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() { hostLookPath = previous })
	fw := &fakeWatcher{}
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	if changed, err := service.ReconcileHostContinuity(); err != nil || !changed {
		t.Fatalf("restart after reboot: %v %v", changed, err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created = %#v, want one new host", fw.created)
	}
}

func TestAutomaticHostContinuityAmbiguousActivationDoesNotDisableLaterRecovery(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "mewla-worker-brain:@ambiguous"
	if err := store.ReplaceHostSessionBinding(hostID, "codex", "saved-thread", "", ""); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Worker{
		hostID: {ID: hostID, Command: "codex", Hidden: true},
	}}
	owned, err := fw.ResolveBrainHostGeneration(hostID)
	if err != nil {
		t.Fatal(err)
	}
	fw.setReceiptOutcome(hostActivationReceipt(hostID, owned.Generation, brainHostContractDigest()), watcher.InputAmbiguous)
	service := NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{"codex": {Command: "codex", Kind: "codex"}}))
	for i := 0; i < 3; i++ {
		if changed, err := service.ReconcileHostContinuity(); changed || !errors.Is(err, ErrHostActivationAmbiguous) {
			t.Fatalf("ambiguous reconcile: %v %v", changed, err)
		}
	}
	if len(fw.sentCalls) != 0 || len(fw.created) != 0 {
		t.Fatal("ambiguous activation was replayed")
	}
	delete(fw.sessions, hostID)
	if changed, err := service.ReconcileHostContinuity(); err != nil || !changed {
		t.Fatalf("later loss could not recover: %v %v", changed, err)
	}
	host, _ := store.HostSession()
	if host.ProviderSessionID != "saved-thread" || len(fw.created) != 1 || len(fw.sentCalls) != 1 {
		t.Fatalf("incorrect recovery: %+v launches=%d sends=%d", host, len(fw.created), len(fw.sentCalls))
	}
}

func mustChatThread(t *testing.T, store *Store) string {
	t.Helper()
	id, err := store.ChatThreadID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
