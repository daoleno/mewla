package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/work"
)

type fakeSessionModels struct{ available bool }

func (f fakeSessionModels) Models(context.Context, *classifier.Worker) (AgentModels, error) {
	return AgentModels{Current: "sonnet"}, nil
}

func (f fakeSessionModels) SetModel(context.Context, *classifier.Worker, string, string) (AgentModels, error) {
	return AgentModels{Current: "sonnet"}, nil
}

func (f fakeSessionModels) ModelsAvailable(*classifier.Worker) bool { return f.available }

// A Claude Session on a Provider route is switched in Claude's own picker:
// the route passes the model Claude asks for through. The Provider path
// cannot reach a running Claude, so it offered no model choice at all
// (11-24: the owner's Claude Sessions, all routed, had no picker).
func TestRoutedClaudeSessionUsesClaudesOwnPicker(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     wsProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	srv := &Server{}
	srv.SetModelProfiles(owner)
	profile := modelprofiles.Profile{
		ID: "claude-gateway", Name: "Gateway", ExecutorID: modelprofiles.ExecutorClaude,
		ProviderID: "custom", ProviderLabel: "Custom Gateway",
		Protocol:              modelprofiles.ProtocolAnthropicMessages,
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		ClientModel:           "claude-sonnet-4-6", Model: "claude-sonnet-4-6",
		BaseURL:       "https://gateway.example",
		AuthMode:      modelprofiles.AuthModeBearerEnv,
		CredentialEnv: "GATEWAY_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorClaude, profile.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "mh-claude"); err != nil {
		t.Fatal(err)
	}
	worker := &classifier.Worker{ID: "mh-claude", Command: "claude --session-id 3311e9d4"}
	if wire := srv.workerSessionWire(worker); wire.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("without Claude's own menu the route offered a switch: %#v", wire.Capabilities)
	}
	srv.SetSessionModels(work.WorkerProviderClaude, fakeSessionModels{available: true})
	if provider, own := srv.sessionModelsProvider(worker); provider != work.WorkerProviderClaude || own == nil {
		t.Fatalf("routed Claude is served by %q", provider)
	}
	if wire := srv.workerSessionWire(worker); !wire.Capabilities.ModelProfileManaged || !wire.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("routed Claude offers no model choice: %#v", wire.Capabilities)
	}
}

// A Session whose agent cannot give it a menu now (not linked yet, not signed
// in, no thread yet) offers no model choice, instead of one that fails.
func TestSessionWithoutItsAgentsMenuOffersNoChoice(t *testing.T) {
	srv := &Server{}
	srv.SetSessionModels(work.WorkerProviderGrok, fakeSessionModels{available: false})
	worker := &classifier.Worker{ID: "mh-grok", Command: "grok"}
	if wire := srv.workerSessionWire(worker); wire.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("a signed-out agent offered a switch: %#v", wire.Capabilities)
	}
	srv.SetSessionModels(work.WorkerProviderGrok, fakeSessionModels{available: true})
	if wire := srv.workerSessionWire(worker); !wire.Capabilities.ModelProfileActiveSwitch {
		t.Fatalf("a ready agent offered no switch: %#v", wire.Capabilities)
	}
}

// The runtime wire carries a pending choice and the models with no default
// effort.
func TestAgentRuntimeSelectionCarriesPendingAndRequiredEffort(t *testing.T) {
	runtime := agentRuntimeSelection("w1", "pi", AgentModels{
		Current: "opencode-go/flash", Effort: "high",
		Models:  []AgentModel{{ID: "opencode-go/flash", Efforts: []string{"low", "high"}, EffortRequired: true}},
		Pending: &AgentChoice{Model: "opencode-go/flash", Effort: "low"},
	})
	if runtime["pending_model_id"] != "opencode-go/flash" || runtime["pending_reasoning_effort"] != "low" {
		t.Fatalf("pending = %v / %v", runtime["pending_model_id"], runtime["pending_reasoning_effort"])
	}
	if _, failed := runtime["failed_model_id"]; failed {
		t.Fatalf("a runtime with no failed pick says %v", runtime["failed_model_id"])
	}
	failed := agentRuntimeSelection("w1", "claude", AgentModels{Current: "sonnet", Failed: &AgentFailure{Model: "opus", Effort: "high", Reason: "Claude's model picker did not show"}})
	if failed["failed_model_id"] != "opus" || failed["failed_reasoning_effort"] != "high" || failed["failed_reason"] != "Claude's model picker did not show" {
		t.Fatalf("failed = %v / %v / %v", failed["failed_model_id"], failed["failed_reasoning_effort"], failed["failed_reason"])
	}
	models := runtime["native_models"].([]map[string]any)
	if models[0]["reasoning_effort_required"] != true {
		t.Fatalf("model = %#v", models[0])
	}
}

// blockingSessionModels answers once released: an agent slow to list its models
// (Claude starting `claude -p` to read them, cold, under load, or not at all).
type blockingSessionModels struct{ release chan struct{} }

func (b blockingSessionModels) Models(ctx context.Context, _ *classifier.Worker) (AgentModels, error) {
	select {
	case <-b.release:
		return AgentModels{Current: "sonnet"}, nil
	case <-ctx.Done():
		return AgentModels{}, ctx.Err()
	}
}

func (b blockingSessionModels) SetModel(ctx context.Context, w *classifier.Worker, _, _ string) (AgentModels, error) {
	return b.Models(ctx, w)
}

func (blockingSessionModels) ModelsAvailable(*classifier.Worker) bool { return true }

// A page asks a Session's model as it opens it, beside its conversation. The
// connection reads one message at a time: a model read that waited on the
// agent held every message behind it, the conversation too (a web reload's
// chat ~5 s late in the 0.4.28 gate; 30 s here with an agent that never
// answered). Other messages are read while it waits.
func TestASlowModelReadHoldsNoOtherMessage(t *testing.T) {
	release := make(chan struct{})
	worker, conn, readRequest := sessionModelsConn(t, blockingSessionModels{release: release})
	if err := conn.WriteJSON(map[string]any{"type": "get_thread_runtime", "request_id": "slow", "worker_id": worker.ID}); err != nil {
		t.Fatal(err)
	}
	// Any message after it: a model read for a Session that has none answers at once.
	if err := conn.WriteJSON(map[string]any{"type": "get_thread_runtime", "request_id": "next", "worker_id": "mh-missing"}); err != nil {
		t.Fatal(err)
	}
	if next := readRequest("next", 3*time.Second); next["type"] != "error" {
		t.Fatalf("the message behind a slow model read = %#v", next)
	}
	close(release)
	if slow := readRequest("slow", 3*time.Second); slow["type"] != "thread_runtime" {
		t.Fatalf("the slow read's answer = %#v", slow)
	}
}

// A switch the agent does not finish is refused with the agent's own reason
// before the app stops waiting for it (20 s): it reached the Model sheet as
// "not acknowledged", with no reason, when the switch had 30 s (11-25).
func TestASwitchTheAgentDoesNotFinishFailsWithinTheAppsWait(t *testing.T) {
	if sessionModelSetBudget >= 18*time.Second {
		t.Fatalf("a switch may take %s; the app waits 20 s for it", sessionModelSetBudget)
	}
	worker, conn, readRequest := sessionModelsConn(t, blockingSessionModels{release: make(chan struct{})})
	started := time.Now()
	if err := conn.WriteJSON(map[string]any{"type": "set_thread_runtime", "request_id": "pick", "worker_id": worker.ID, "runtime": map[string]any{"connection_id": "claude", "model_id": "opus"}}); err != nil {
		t.Fatal(err)
	}
	reply := readRequest("pick", 25*time.Second)
	if reply["type"] != "error" || time.Since(started) >= 18*time.Second {
		t.Fatalf("after %s the switch answered %#v", time.Since(started).Round(time.Second), reply)
	}
}

// sessionModelsConn is a paired app connection to a daemon whose Claude
// Sessions' menus come from models.
func sessionModelsConn(t *testing.T, models SessionModels) (*classifier.Worker, *websocket.Conn, func(string, time.Duration) map[string]any) {
	t.Helper()
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	deviceID := "device-models"
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	worker := &classifier.Worker{ID: "mh-slow", Command: "claude --session-id 3311e9d4", State: classifier.StateRunning}
	srv.getWorkerOverride = func(id string) *classifier.Worker {
		if id == worker.ID {
			return worker
		}
		return nil
	}
	srv.SetSessionModels(work.WorkerProviderClaude, models)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(httpServer.Close)
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	readRequest := func(want string, wait time.Duration) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("waiting for %s: %v", want, err)
			}
			var payload map[string]any
			if json.Unmarshal(raw, &payload) == nil && payload["request_id"] == want {
				return payload
			}
		}
	}
	return worker, conn, readRequest
}
