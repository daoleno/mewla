package modelprofiles

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSettingsEnv(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("settings is not JSON: %v\n%s", err, raw)
	}
	env, _ := doc["env"].(map[string]any)
	return env
}

func TestClaudeTakeoverPreservesAndRestoresUserSettings(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "claude", "settings.json")
	original := []byte(`{
  "model": "opus",
  "env": {"ANTHROPIC_BASE_URL": "https://mine.example", "OTHER": "1"},
  "permissions": {"allow": ["Bash(ls)"]}
}`)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	takeover := NewClaudeTakeover(settings, filepath.Join(root, "state"))

	if err := takeover.Project("http://127.0.0.1:3425", true); err != nil {
		t.Fatal(err)
	}
	env := readSettingsEnv(t, settings)
	if env[EnvAnthropicBaseURL] != "http://127.0.0.1:3425" || env[EnvAnthropicAuthToken] != LoopbackAuthPlaceholder || env["OTHER"] != "1" {
		t.Fatalf("projected env = %#v", env)
	}
	raw, _ := os.ReadFile(settings)
	if !strings.Contains(string(raw), `"Bash(ls)"`) || !strings.Contains(string(raw), `"model": "opus"`) {
		t.Fatalf("unrelated keys were lost:\n%s", raw)
	}
	if info, _ := os.Stat(settings); info.Mode().Perm() != 0o644 {
		t.Fatalf("settings mode changed to %v", info.Mode().Perm())
	}
	state, err := takeover.loadState()
	if err != nil || state.BackupPath == "" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	backup, err := os.ReadFile(state.BackupPath)
	if err != nil || string(backup) != string(original) {
		t.Fatalf("backup is not the exact original: %v\n%s", err, backup)
	}

	// Port fallback moves the gateway: re-projection keeps the user's prior.
	if err := takeover.Project("http://127.0.0.1:3426", false); err != nil {
		t.Fatal(err)
	}
	env = readSettingsEnv(t, settings)
	if env[EnvAnthropicBaseURL] != "http://127.0.0.1:3426" {
		t.Fatalf("re-projected base = %#v", env)
	}
	if _, ok := env[EnvAnthropicAuthToken]; ok {
		t.Fatalf("native passthrough must not carry the placeholder token: %#v", env)
	}

	if err := takeover.Release(); err != nil {
		t.Fatal(err)
	}
	env = readSettingsEnv(t, settings)
	if env[EnvAnthropicBaseURL] != "https://mine.example" || env["OTHER"] != "1" {
		t.Fatalf("release did not restore the user's value: %#v", env)
	}
	if _, ok := env[EnvAnthropicAuthToken]; ok {
		t.Fatalf("release left Mewla's token behind: %#v", env)
	}
	if takeover.Enabled() {
		t.Fatal("takeover still enabled after release")
	}
}

func TestClaudeTakeoverReleaseKeepsUserEditsAndCreatesMissingFile(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "claude", "settings.json")
	takeover := NewClaudeTakeover(settings, filepath.Join(root, "state"))
	if err := takeover.Project("http://127.0.0.1:3425", true); err != nil {
		t.Fatal(err)
	}
	if env := readSettingsEnv(t, settings); env[EnvAnthropicBaseURL] != "http://127.0.0.1:3425" {
		t.Fatalf("missing settings file was not created: %#v", env)
	}
	// The user points Claude elsewhere by hand after the takeover.
	if err := os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://edited.example","ANTHROPIC_AUTH_TOKEN":"`+LoopbackAuthPlaceholder+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := takeover.Release(); err != nil {
		t.Fatal(err)
	}
	env := readSettingsEnv(t, settings)
	if env[EnvAnthropicBaseURL] != "https://edited.example" {
		t.Fatalf("release clobbered a user edit: %#v", env)
	}
	if _, ok := env[EnvAnthropicAuthToken]; ok {
		t.Fatalf("release left Mewla's untouched token behind: %#v", env)
	}
}

func TestClaudeTakeoverRefusesMalformedSettingsAndFollowsSymlink(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"env": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	takeover := NewClaudeTakeover(settings, filepath.Join(root, "state"))
	if err := takeover.Project("http://127.0.0.1:3425", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed settings error = %v", err)
	}
	if raw, _ := os.ReadFile(settings); string(raw) != `{"env": [` {
		t.Fatalf("malformed settings were rewritten: %s", raw)
	}

	target := filepath.Join(root, "dotfiles", "claude-settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	linked := NewClaudeTakeover(link, filepath.Join(root, "state-linked"))
	if err := linked.Project("http://127.0.0.1:3425", true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was replaced: %v %v", info, err)
	}
	if env := readSettingsEnv(t, target); env[EnvAnthropicBaseURL] != "http://127.0.0.1:3425" {
		t.Fatalf("symlink target not projected: %#v", env)
	}
}

// TestClaudeGatewayPassesThroughToSelectedConnection: a Claude started from a
// plain shell reaches the gateway through the settings projection; the gateway
// forwards to the selected Claude connection whatever model is asked for,
// swaps the placeholder for the real credential, and follows the selection.
// The Codex upstream is untouched by Claude switches.
func TestClaudeGatewayPassesThroughToSelectedConnection(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "claude", "settings.json")
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:       filepath.Join(root, "profiles.toml"),
		RoutesPath:         filepath.Join(root, "routes.json"),
		ListenerPath:       filepath.Join(root, "listener.json"),
		GatewayAddr:        "127.0.0.1:0",
		GatewayStateDir:    filepath.Join(root, "gateway"),
		CodexConfigPath:    filepath.Join(root, "codex", "config.toml"),
		ClaudeSettingsPath: settings,
		Credentials:        NewMemoryCredentialStore(),
		Verifier:           BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	type hit struct{ name, path, model, auth, apiKey, body string }
	hits := make(chan hit, 8)
	newUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(raw, &body)
			hits <- hit{name, r.URL.Path, body.Model, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), string(raw)}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"type":"message"}`)
		}))
	}
	first := newUpstream("first")
	second := newUpstream("second")
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)

	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "claude-first", Name: "first", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: first.URL, Advanced: true,
	}, "first-key", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "claude-second", Name: "second", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: second.URL, Advanced: true,
	}, "second-key", proj.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "codex-conn", Name: "codex", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://codex.example/v1", ModelID: "gpt-5", Advanced: true,
	}, "codex-key", proj.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if proj, err = owner.SetProviderConnection("codex", "codex-conn", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if proj, err = owner.SetProviderConnection("claude", "claude-first", proj.Revision); err != nil {
		t.Fatal(err)
	}

	gatewayURL := "http://" + owner.Gateway().ActualAddr()
	env := readSettingsEnv(t, settings)
	if env[EnvAnthropicBaseURL] != gatewayURL || env[EnvAnthropicAuthToken] != LoopbackAuthPlaceholder {
		t.Fatalf("settings projection = %#v, want gateway %s", env, gatewayURL)
	}

	send := func(path, body string) hit {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, gatewayURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
		req.Header.Set("X-Api-Key", LoopbackClaudeAPIKeyPlaceholder)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, raw)
		}
		select {
		case h := <-hits:
			return h
		default:
			t.Fatalf("%s reached no upstream", path)
		}
		return hit{}
	}
	assertCredential := func(h hit, key string) {
		t.Helper()
		if strings.Contains(h.auth+h.apiKey, LoopbackAuthPlaceholder) || !strings.Contains(h.auth+h.apiKey, key) {
			t.Fatalf("upstream auth = %q / %q, want %s only", h.auth, h.apiKey, key)
		}
	}

	// A background model no catalog lists still passes through untouched.
	body := `{"model":"claude-haiku-off-catalog","max_tokens":1,"messages":[]}`
	h := send("/v1/messages", body)
	if h.name != "first" || h.path != "/v1/messages" || h.body != body {
		t.Fatalf("messages hit = %#v", h)
	}
	assertCredential(h, "first-key")

	h = send("/v1/messages/count_tokens", `{"model":"claude-opus-4-6[1m]","messages":[]}`)
	if h.name != "first" || h.path != "/v1/messages/count_tokens" || h.model != "claude-opus-4-6" {
		t.Fatalf("count_tokens hit = %#v", h)
	}

	if _, err := owner.SetProviderConnection("claude", "claude-second", proj.Revision); err != nil {
		t.Fatal(err)
	}
	h = send("/v1/messages", body)
	if h.name != "second" {
		t.Fatalf("gateway did not follow the Claude selection: %#v", h)
	}
	assertCredential(h, "second-key")
	if up, ok := owner.Gateway().Upstream(); !ok || up.ProfileID != "codex-conn" {
		t.Fatalf("Claude switch moved the Codex upstream: %#v ok=%v", up, ok)
	}
	if env := readSettingsEnv(t, settings); env[EnvAnthropicBaseURL] != gatewayURL {
		t.Fatalf("projection drifted across the switch: %#v", env)
	}
}

// TestGatewayTakeoverFollowsSelection: with no Provider selected (Official
// login) neither client config is touched; selecting a connection projects the
// gateway with no enable step, and clearing the selection gives it back.
func TestGatewayTakeoverFollowsSelection(t *testing.T) {
	root := t.TempDir()
	codexConfig := filepath.Join(root, "codex", "config.toml")
	settings := filepath.Join(root, "claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(codexConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexConfig, []byte("model = \"user-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:       filepath.Join(root, "profiles.toml"),
		RoutesPath:         filepath.Join(root, "routes.json"),
		ListenerPath:       filepath.Join(root, "listener.json"),
		GatewayAddr:        "127.0.0.1:0",
		GatewayStateDir:    filepath.Join(root, "gateway"),
		CodexConfigPath:    codexConfig,
		ClaudeSettingsPath: settings,
		Credentials:        NewMemoryCredentialStore(),
		Verifier:           BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	codexProjected := func() bool {
		raw, err := os.ReadFile(codexConfig)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Contains(string(raw), GatewayProviderName)
	}
	if codexProjected() {
		t.Fatal("Official-login Codex config was taken over")
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatalf("Official-login Claude settings were written: %v", err)
	}

	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "codex-conn", Name: "codex", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://codex.example/v1", ModelID: "gpt-5", Advanced: true,
	}, "codex-key", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "claude-conn", Name: "claude", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: "https://claude.example", Advanced: true,
	}, "claude-key", proj.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if proj, err = owner.SetProviderConnection("codex", "codex-conn", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if proj, err = owner.SetProviderConnection("claude", "claude-conn", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if !codexProjected() {
		t.Fatal("selecting a Codex Provider did not project the gateway")
	}
	if env := readSettingsEnv(t, settings); env[EnvAnthropicBaseURL] != "http://"+owner.Gateway().ActualAddr() {
		t.Fatalf("selecting a Claude Provider did not project the gateway: %#v", env)
	}

	if proj, err = owner.SetProviderConnection("codex", "", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.SetProviderConnection("claude", "", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if codexProjected() {
		t.Fatal("Official login did not give the Codex config back")
	}
	raw, _ := os.ReadFile(codexConfig)
	if !strings.Contains(string(raw), `model = "user-model"`) {
		t.Fatalf("user Codex config lost: %s", raw)
	}
	if env := readSettingsEnv(t, settings); len(env) != 0 {
		t.Fatalf("Official login did not give the Claude settings back: %#v", env)
	}
}
