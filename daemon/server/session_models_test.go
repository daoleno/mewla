package server

import (
	"context"
	"path/filepath"
	"testing"

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
	models := runtime["native_models"].([]map[string]any)
	if models[0]["reasoning_effort_required"] != true {
		t.Fatalf("model = %#v", models[0])
	}
}
