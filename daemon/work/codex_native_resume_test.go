package work

import (
	"strings"
	"testing"
)

func TestCodexNativeResumePreservesSessionChoicesAndConnection(t *testing.T) {
	command := `env ROUTE_OWNER=brain codex --model stale -c 'model_reasoning_effort="low"' -c 'model_provider="owned"' --dangerously-bypass-approvals-and-sandbox`
	got, err := CodexNativeResumeCommand(command, "exact-thread")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "stale") || strings.Contains(got, "model_reasoning_effort") {
		t.Fatalf("defaults override saved session: %s", got)
	}
	for _, want := range []string{"ROUTE_OWNER=brain", `model_provider="owned"`, CodexFullAuthorizationFlag} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost connection or permissions %q: %s", want, got)
		}
	}
	if token, found, err := ProviderResumeToken("codex", got); err != nil || !found || token != "exact-thread" {
		t.Fatalf("resume identity: %q %v %v", token, found, err)
	}
}
