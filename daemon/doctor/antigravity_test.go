package doctor

import (
	"errors"
	"os/exec"
	"testing"
)

func TestParseAntigravityAuth(t *testing.T) {
	exit1 := exec.Command("sh", "-c", "exit 1").Run()
	var exitErr *exec.ExitError
	if !errors.As(exit1, &exitErr) {
		t.Fatalf("fixture exit error = %v", exit1)
	}
	// agy 1.5.14: `agy models` signed out prints this and exits 1.
	signedOut := []byte("Fetching available models...\nError: Please sign in to view available models. Launch the CLI without arguments to sign in.\n")
	if got := parseAntigravityAuth(signedOut, exit1); got != AuthUnauthenticated {
		t.Fatalf("signed out = %v", got)
	}
	list := []byte("Fetching available models...\ngemini-4-neon-high\tGemini 4 Neon (High)\n")
	if got := parseAntigravityAuth(list, nil); got != AuthAuthenticated {
		t.Fatalf("model list = %v", got)
	}
	// The list is the account's own: other vendors' ids are rows too.
	other := []byte("Fetching available models...\nclaude-sonnet-5-5\tClaude Sonnet 5.5\ngpt-oss-120b\tGPT-OSS 120B\n")
	if got := parseAntigravityAuth(other, nil); got != AuthAuthenticated {
		t.Fatalf("non-Google ids = %v", got)
	}
	if got := parseAntigravityAuth([]byte("Fetching available models...\n"), nil); got != AuthUnknown {
		t.Fatalf("no models = %v", got)
	}
	// No network, a timeout, an unknown message: never a guess.
	if got := parseAntigravityAuth([]byte("dial tcp: i/o timeout"), exit1); got != AuthUnknown {
		t.Fatalf("other failure = %v", got)
	}
	if got := parseAntigravityAuth(nil, nil); got != AuthUnknown {
		t.Fatalf("empty success = %v", got)
	}
}

func TestRecommendHostNeverPicksAWorkerOnlyAgent(t *testing.T) {
	only := []ExecutorCheck{{ID: "agy", Provider: "antigravity"}}
	if got := recommendHost(only); got != "" {
		t.Fatalf("agy alone cannot host Brain, got %q", got)
	}
	both := []ExecutorCheck{{ID: "agy", Provider: "antigravity"}, {ID: "pi", Provider: "pi"}}
	if got := recommendHost(both); got != "pi" {
		t.Fatalf("host = %q, want pi", got)
	}
}
