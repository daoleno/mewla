package modelprofiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setClaudeEnv(t *testing.T, path, key, value string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	env, _ := doc["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env[key] = value
	doc["env"] = env
	out, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeCoexistFixture(t *testing.T) (*ClaudeTakeover, string) {
	t.Helper()
	root := t.TempDir()
	settings := filepath.Join(root, "claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"env": {"ANTHROPIC_BASE_URL": "https://mine.example"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewClaudeTakeover(settings, filepath.Join(root, "state")), settings
}

// Magpie's Claude Code setting on while Mewla's is on, Mewla's off, Magpie's
// off: Magpie writes back the address it saw, Mewla's. Heal gives Claude Code
// the user's own address back.
func TestClaudeHealAfterAnotherToolRestoresMewlasAddress(t *testing.T) {
	takeover, settings := claudeCoexistFixture(t)
	if err := takeover.Project("http://127.0.0.1:3425", true, true); err != nil {
		t.Fatal(err)
	}
	setClaudeEnv(t, settings, EnvAnthropicBaseURL, "http://127.0.0.1:3435") // Magpie connects
	if err := takeover.Release(); err != nil {
		t.Fatal(err)
	}
	if env := readSettingsEnv(t, settings); env[EnvAnthropicBaseURL] != "http://127.0.0.1:3435" {
		t.Fatalf("release took Claude Code from Magpie: %#v", env)
	}
	setClaudeEnv(t, settings, EnvAnthropicBaseURL, "http://127.0.0.1:3425") // Magpie disconnects
	takeover.Heal()
	env := readSettingsEnv(t, settings)
	if env[EnvAnthropicBaseURL] != "https://mine.example" {
		t.Fatalf("heal left %#v", env)
	}
	if _, has := env[EnvAnthropicAuthToken]; has {
		t.Fatalf("heal left Mewla's placeholder token: %#v", env)
	}
}

// A daemon restart (a background projection) never takes Claude Code from
// another tool; choosing a Claude Provider in Mewla does.
func TestClaudeBackgroundProjectionYieldsToAnotherTool(t *testing.T) {
	takeover, settings := claudeCoexistFixture(t)
	if err := takeover.Project("http://127.0.0.1:3425", true, true); err != nil {
		t.Fatal(err)
	}
	setClaudeEnv(t, settings, EnvAnthropicBaseURL, "http://127.0.0.1:3435")
	if err := takeover.Project("http://127.0.0.1:3425", true, false); err != nil {
		t.Fatal(err)
	}
	if env := readSettingsEnv(t, settings); env[EnvAnthropicBaseURL] != "http://127.0.0.1:3435" {
		t.Fatalf("a restart took Claude Code from Magpie: %#v", env)
	}
	if err := takeover.Project("http://127.0.0.1:3425", true, true); err != nil {
		t.Fatal(err)
	}
	if env := readSettingsEnv(t, settings); env[EnvAnthropicBaseURL] != "http://127.0.0.1:3425" {
		t.Fatalf("choosing a Provider did not take Claude Code back: %#v", env)
	}
}

// The running daemon notices another tool's write and heals it without a
// restart or a Provider change.
func TestOwnerHealsTheCodexConfigWhenAnotherToolWritesIt(t *testing.T) {
	old := clientConfigWatchInterval
	clientConfigWatchInterval = 20 * time.Millisecond
	t.Cleanup(func() { clientConfigWatchInterval = old })
	root := t.TempDir()
	codexConfig := filepath.Join(root, "codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexConfig, []byte(coexistUserConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "gateway")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The takeover was on and let go: the user's provider is known.
	off, _ := json.Marshal(TakeoverState{ConfigPath: codexConfig, ProviderName: GatewayProviderName, OriginalProviderValue: "freeride-xcode", OriginalProviderHadTable: true})
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), off, 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:    filepath.Join(root, "profiles.toml"),
		RoutesPath:      filepath.Join(root, "routes.json"),
		ListenerPath:    filepath.Join(root, "listener.json"),
		GatewayAddr:     "127.0.0.1:0",
		GatewayStateDir: stateDir,
		CodexConfigPath: codexConfig,
		Credentials:     NewMemoryCredentialStore(),
		Lookup:          readyLookup("secret"),
		Verifier:        BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	magpie := &fakeMagpie{t: t, path: codexConfig, left: GatewayProviderName}
	magpie.disconnect() // writes back the mewla-gateway it remembered
	deadline := time.Now().Add(5 * time.Second)
	for {
		if name, ok := codexWouldStart(t, codexConfig); ok && name == "freeride-xcode" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon left Codex on a dangling provider:\n%s", readFileBytes(t, codexConfig))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
