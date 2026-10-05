package modelprofiles

import (
	"errors"
	"testing"
)

func TestClaudeProviderSelectionRetargetsLiveRoute(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	first := claudeMessagesProfile("claude-first", "claude-sonnet-4-6", "claude-sonnet-4-6")
	second := claudeMessagesProfile("claude-second", "claude-sonnet-4-6", "claude-sonnet-4-6")
	second.BaseURL = "https://another.example"
	for _, profile := range []Profile{first, second} {
		if _, err := owner.UpsertProfile(profile, owner.Catalog().Revision, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.SetProviderConnection(ClientClaude, first.ID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, first.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "claude-session"); err != nil {
		t.Fatal(err)
	}
	before, _ := owner.Table().Get("claude-session")
	if caps := CapabilitiesFor(ExecutorClaude); len(caps.Protocols) != 1 || caps.Protocols[0].ActiveSwitch != "" {
		t.Fatalf("Claude must not advertise native hot switching: %#v", caps)
	}
	if caps := owner.SessionRouteCapabilities("claude-session"); !caps.Managed || caps.ActiveSwitch {
		t.Fatalf("managed Claude should be read-only: %#v", caps)
	}
	if runtime, ok := owner.ThreadRuntime("claude-session"); !ok || runtime.HotSwitchable {
		t.Fatalf("Claude runtime advertised an unapplied mutation: %#v", runtime)
	}
	if _, err := owner.PrepareThreadRuntime("claude-session", ThreadRuntimeChoice{
		ConnectionID: second.ID, ModelID: second.Model,
	}); !errors.Is(err, ErrBindingNotRouted) {
		t.Fatalf("prepared route-only Claude mutation: %v", err)
	}
	if _, _, _, err := owner.ActivateSession("claude-session", second.ID, before.Generation); !errors.Is(err, ErrBindingNotRouted) {
		t.Fatalf("activated route-only Claude mutation: %v", err)
	}
	// Selecting a Claude Provider retargets the live route in place: the CLI
	// keeps its loopback URL and client model, the next request reaches the
	// newly selected upstream.
	projection, err := owner.SetProviderConnection(ClientClaude, second.ID, owner.Catalog().Revision)
	if err != nil || projection.Defaults[ClientClaude].ConnectionID != second.ID {
		t.Fatalf("select Claude Provider: projection=%#v err=%v", projection.Defaults, err)
	}
	after, _ := owner.Table().Get("claude-session")
	if after.Binding.ProfileID != second.ID || after.Binding.UpstreamBaseURL != second.BaseURL {
		t.Fatalf("live Claude route not retargeted: %#v", after.Binding)
	}
	if after.Generation != before.Generation+1 || after.Binding.RouteID != before.Binding.RouteID {
		t.Fatalf("retarget must keep the route and bump generation: before=%#v after=%#v", before, after)
	}
	if after.Binding.ClientModel != before.Binding.ClientModel {
		t.Fatalf("client model changed: %q -> %q", before.Binding.ClientModel, after.Binding.ClientModel)
	}
	if _, err := owner.SwitchProvider(ClientClaude, first.ID, owner.Catalog().Revision); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if back, _ := owner.Table().Get("claude-session"); back.Binding.ProfileID != first.ID {
		t.Fatalf("switch back not applied: %#v", back.Binding)
	}
}
