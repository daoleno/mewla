package server

import (
	"encoding/json"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

func hostPromptWorker(hostID string, state classifier.WorkerState, promptID string) *classifier.Worker {
	worker := &classifier.Worker{ID: hostID, Name: "Brain", Command: "claude", Hidden: true, State: state}
	if promptID != "" {
		worker.Prompt = &classifier.Prompt{
			ID: promptID, Kind: classifier.PromptPermission, Provider: "claude", Title: "Bash command",
			Options: []classifier.PromptOption{{Number: 1, Label: "Yes", Selected: true}, {Number: 2, Label: "No"}},
		}
	}
	return worker
}

// asWire is what a phone receives: the message after JSON.
func asWire(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBrainSnapshotHostWorkerCarriesBlockingPrompt(t *testing.T) {
	srv := &Server{}
	hostID := "mewla-worker-brain-hidden:@prompt"
	current := hostPromptWorker(hostID, classifier.StateBlocked, "perm-1")
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		if id == hostID {
			return current
		}
		return nil
	}

	host, _ := asWire(t, mustBrainSnapshotHostWire(t, srv, hostID, "claude"))["host_worker"].(map[string]any)
	prompt, ok := host["prompt"].(map[string]any)
	if !ok || prompt["id"] != "perm-1" || prompt["kind"] != "permission" {
		t.Fatalf("blocked host prompt = %#v", host["prompt"])
	}
	if options, _ := prompt["options"].([]any); len(options) != 2 {
		t.Fatalf("prompt options = %#v", prompt["options"])
	}

	// A prompt that outlived the block is not a prompt to answer.
	current = hostPromptWorker(hostID, classifier.StateRunning, "perm-1")
	host, _ = asWire(t, mustBrainSnapshotHostWire(t, srv, hostID, "claude"))["host_worker"].(map[string]any)
	if _, has := host["prompt"]; has {
		t.Fatalf("running host carries prompt %#v", host["prompt"])
	}
	current = hostPromptWorker(hostID, classifier.StateBlocked, "")
	host, _ = asWire(t, mustBrainSnapshotHostWire(t, srv, hostID, "claude"))["host_worker"].(map[string]any)
	if _, has := host["prompt"]; has {
		t.Fatalf("host without a prompt carries %#v", host["prompt"])
	}
}

func TestHiddenHostBlockChangesRefreshBrainSnapshot(t *testing.T) {
	hostID := "mewla-worker-brain-hidden:@blocks"
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession(hostID, "claude"); err != nil {
		t.Fatal(err)
	}
	bw := &killTrackingWatcher{}
	bw.sessions = map[string]*classifier.Worker{}
	service := brain.NewService(store, bw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{"claude": {Name: "claude", Command: "claude", Kind: "claude"}},
	})
	srv := &Server{brain: service}
	var broadcasts []map[string]any
	srv.brainSnapshotBroadcastHook = func(payload map[string]any) { broadcasts = append(broadcasts, payload) }
	srv.getWorkerOverride = func(id string) *classifier.Worker { return bw.GetWorker(id) }

	send := func(eventType string, worker *classifier.Worker) {
		bw.sessions[hostID] = worker
		srv.handleWatcherEvent(watcher.SessionEvent{Type: eventType, WorkerID: hostID, Worker: worker})
	}
	promptOf := func(index int) any {
		brainPayload, _ := asWire(t, broadcasts[index])["brain"].(map[string]any)
		host, _ := brainPayload["host_worker"].(map[string]any)
		prompt, _ := host["prompt"].(map[string]any)
		if prompt == nil {
			return nil
		}
		return prompt["id"]
	}

	// Plain running noise changes nothing a phone can act on.
	send("worker_state_change", hostPromptWorker(hostID, classifier.StateRunning, ""))
	send("worker_metadata_change", hostPromptWorker(hostID, classifier.StateRunning, ""))
	if len(broadcasts) != 0 {
		t.Fatalf("running host churned brain_snapshot: %d", len(broadcasts))
	}

	send("worker_state_change", hostPromptWorker(hostID, classifier.StateBlocked, "perm-1"))
	if len(broadcasts) != 1 || promptOf(0) != "perm-1" {
		t.Fatalf("blocking on a prompt: broadcasts=%d prompt=%v", len(broadcasts), promptOf(len(broadcasts)-1))
	}
	// The same prompt seen again is not news.
	send("worker_metadata_change", hostPromptWorker(hostID, classifier.StateBlocked, "perm-1"))
	if len(broadcasts) != 1 {
		t.Fatalf("same prompt re-broadcast: %d", len(broadcasts))
	}
	// A different prompt in the same block replaces the card.
	send("worker_metadata_change", hostPromptWorker(hostID, classifier.StateBlocked, "plan-1"))
	if len(broadcasts) != 2 || promptOf(1) != "plan-1" {
		t.Fatalf("next prompt: broadcasts=%d prompt=%v", len(broadcasts), promptOf(len(broadcasts)-1))
	}
	// Answered: the card goes away.
	send("worker_state_change", hostPromptWorker(hostID, classifier.StateRunning, ""))
	if len(broadcasts) != 3 || promptOf(2) != nil {
		t.Fatalf("answered: broadcasts=%d prompt=%v", len(broadcasts), promptOf(len(broadcasts)-1))
	}
	send("worker_state_change", hostPromptWorker(hostID, classifier.StateRunning, ""))
	if len(broadcasts) != 3 {
		t.Fatalf("settled host churned brain_snapshot: %d", len(broadcasts))
	}

	// Another hidden Session blocking is not the host.
	other := hostPromptWorker("mewla-worker-other-hidden:@1", classifier.StateBlocked, "perm-9")
	bw.sessions[other.ID] = other
	srv.handleWatcherEvent(watcher.SessionEvent{Type: "worker_state_change", WorkerID: other.ID, Worker: other})
	if len(broadcasts) != 3 {
		t.Fatalf("other hidden Session churned brain_snapshot: %d", len(broadcasts))
	}
}
