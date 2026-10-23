package modelprofiles

import (
	"errors"
	"strings"
	"testing"
)

func TestOnlyDeepSeekResponsesRoutesHaveARequestAdapter(t *testing.T) {
	for _, tc := range []struct {
		provider, protocol string
		adapted            bool
	}{
		{"deepseek", RouteProtocolResponses, true},
		{" DeepSeek ", RouteProtocolResponses, true},
		{"deepseek", "anthropic_messages", false},
		{"openai", RouteProtocolResponses, false},
		{"custom", RouteProtocolResponses, false},
		{"", RouteProtocolResponses, false},
	} {
		got := responsesAdapterFor(RouteBinding{ProviderID: tc.provider, RouteProtocol: tc.protocol}) != nil
		if got != tc.adapted {
			t.Errorf("%q/%q adapted = %v, want %v", tc.provider, tc.protocol, got, tc.adapted)
		}
	}
}

func TestTheDeepSeekAdapterIsTheDeepSeekSanitizer(t *testing.T) {
	adapt := responsesAdapterFor(RouteBinding{ProviderID: "deepseek", RouteProtocol: RouteProtocolResponses})
	body := []byte(`{"model":"deepseek-v4-flash","input":"hi","previous_response_id":"resp_1","service_tier":"auto"}`)
	want, wantErr := sanitizeDeepSeekResponsesRequest(body, envelopeDeepSeekV4Flash())
	got, err := adapt(body)
	if string(got) != string(want) || (err == nil) != (wantErr == nil) {
		t.Fatalf("adapter = %s / %v, sanitizer = %s / %v", got, err, want, wantErr)
	}
	if _, err := adapt([]byte(`{"model":"deepseek-v4-flash","input":"hi","background":true}`)); err != nil && !errors.Is(err, ErrResponsesFeatureUnsupported) {
		t.Logf("unsupported feature reports %v", err)
	}
	if _, err := adapt([]byte(`not json`)); err == nil || errors.Is(err, ErrResponsesFeatureUnsupported) {
		t.Fatalf("malformed body: %v", err)
	}
}

// One provider serving two protocols at two paths is data on its preset, not
// a branch in the compiler.
func TestPresetVariantsShapeTheClaudeConnection(t *testing.T) {
	for _, tc := range []struct {
		baseURL, want string
	}{
		{"", "https://api.deepseek.com/anthropic"},
		{"https://api.deepseek.com", "https://api.deepseek.com/anthropic"},
		{"https://api.deepseek.com/", "https://api.deepseek.com/anthropic"},
		{"https://gateway.example/deepseek", "https://gateway.example/deepseek/anthropic"},
		{"https://gateway.example/anthropic", "https://gateway.example/anthropic"},
	} {
		claude, err := compileProviderConnectionForClient(ProviderConnectionInput{ID: "ds", Name: "DeepSeek", PresetID: ProviderPresetDeepSeek, BaseURL: tc.baseURL, Advanced: tc.baseURL != "" && !strings.HasPrefix(tc.baseURL, "https://api.deepseek.com")}, ExecutorClaude)
		if err != nil {
			t.Fatalf("%q: %v", tc.baseURL, err)
		}
		if claude.BaseURL != tc.want || claude.Protocol != ProtocolAnthropicMessages || claude.AuthMode != AuthModeXAPIKeyEnv {
			t.Errorf("%q: claude = %s %s %s", tc.baseURL, claude.BaseURL, claude.Protocol, claude.AuthMode)
		}
	}
	codex, err := compileProviderConnectionForClient(ProviderConnectionInput{ID: "ds", Name: "DeepSeek", PresetID: ProviderPresetDeepSeek}, ExecutorCodex)
	if err != nil {
		t.Fatal(err)
	}
	if codex.BaseURL != "https://api.deepseek.com" || codex.Protocol != ProtocolOpenAIResponses || codex.AuthMode != AuthModeBearerEnv || codex.CredentialEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("codex = %s %s %s %s", codex.BaseURL, codex.Protocol, codex.AuthMode, codex.CredentialEnv)
	}
}
