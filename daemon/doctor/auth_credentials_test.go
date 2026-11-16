package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

// A Codex that runs on its own provider or an API key is not "signed out"
// because codex login status says so; a Codex with neither still is.
func TestCodexOnOwnCredentialsIsNotSignedOut(t *testing.T) {
	dir := t.TempDir()
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderCodex, dir); got != AuthUnauthenticated {
		t.Fatalf("no credentials: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("model_provider = \"openai\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderCodex, dir); got != AuthUnauthenticated {
		t.Fatalf("OpenAI's own provider: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("model_provider = \"magpie\"\n[model_providers.magpie]\nbase_url = \"https://example.invalid/v1\"\nenv_key = \"MAGPIE_KEY\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderCodex, dir); got != AuthUnknown {
		t.Fatalf("own provider: %s", got)
	}
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderCodex, t.TempDir()); got != AuthUnknown {
		t.Fatalf("API key: %s", got)
	}
	if got := unlessOtherCredentials(AuthAuthenticated, work.WorkerProviderCodex, dir); got != AuthAuthenticated {
		t.Fatalf("signed in stays signed in: %s", got)
	}
}

func TestClaudeOnOwnCredentialsIsNotSignedOut(t *testing.T) {
	dir := t.TempDir()
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderClaude, dir); got != AuthUnauthenticated {
		t.Fatalf("no credentials: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"apiKeyHelper":"~/bin/key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderClaude, dir); got != AuthUnknown {
		t.Fatalf("apiKeyHelper: %s", got)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	if got := unlessOtherCredentials(AuthUnauthenticated, work.WorkerProviderClaude, t.TempDir()); got != AuthUnknown {
		t.Fatalf("API key: %s", got)
	}
}
