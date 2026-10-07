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
	const hostID = "zen-worker-brain:@original"
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
	}{
		{"missing native identity", "", nil},
		{"unknown tmux liveness", "saved-thread", errors.New("socket unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ReplaceHostSessionBinding("missing:@1", "codex", tc.session, "", ""); err != nil {
				t.Fatal(err)
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

func TestAutomaticHostContinuityAmbiguousActivationDoesNotDisableLaterRecovery(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hostID = "zen-worker-brain:@ambiguous"
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
