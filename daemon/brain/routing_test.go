package brain

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestRoutingGuideSeedsOnceAndSurvivesHousekeeping(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := os.ReadFile(store.routingGuidePath())
	if err != nil || string(seeded) != defaultRoutingGuide {
		t.Fatalf("fresh workspace routing.md = %q, err=%v", seeded, err)
	}
	if len(seeded) > 2048 {
		t.Fatalf("routing seed grew to %d bytes", len(seeded))
	}

	edited := "# Worker Routing\n\n- Frontend: claude, high.\n"
	if err := os.WriteFile(store.routingGuidePath(), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	report, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(store.routingGuidePath()); string(got) != edited {
		t.Fatalf("Housekeeping rewrote the user's routing.md:\n%s", got)
	}
	if slices.Contains(report.ChangedPaths, routingGuideName) || slices.Contains(report.UnmanagedPaths, routingGuideName) {
		t.Fatalf("routing.md reported changed=%v unmanaged=%v", report.ChangedPaths, report.UnmanagedPaths)
	}
	if _, err := NewStore(store.Root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(store.routingGuidePath()); string(got) != edited {
		t.Fatalf("store reopen rewrote routing.md:\n%s", got)
	}

	// Brain is told to read it before spawning and to maintain it.
	agents, _ := os.ReadFile(store.workspaceInstructionsPath())
	engine, _ := os.ReadFile(store.policyPath("engine.md"))
	if !strings.Contains(string(agents), "routing.md and policies/engine.md before every Worker spawn") ||
		!strings.Contains(string(engine), "Revise it when the user states a routing preference") {
		t.Fatalf("prompts do not route through routing.md:\nAGENTS:\n%s\nengine:\n%s", agents, engine)
	}
}
