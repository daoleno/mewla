package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeHistoryDropsWhatTranslationWrote(t *testing.T) {
	sealed := sealAnthropicThinking("plan", "SIG", "")
	codex := `{"model":"gpt-5.5","input":[` +
		`{"type":"message","role":"user","content":"hi"},` +
		`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"` + sealed + `"},` +
		`{"type":"reasoning","id":"rs_2","summary":[],"encrypted_content":"gAAAAopenai"},` +
		`{"type":"function_call","call_id":"c1","name":"exec","arguments":"{}"}]}`
	out := string(NativeHistory(APIResponses, []byte(codex)))
	if strings.Contains(out, anthropicSealPrefix) || !strings.Contains(out, "gAAAAopenai") || !strings.Contains(out, `"call_id":"c1"`) {
		t.Fatalf("got %s: want only Mewla's sealed item gone", out)
	}

	claude := `{"model":"claude-opus-5-5","max_tokens":9,"thinking":{"type":"enabled","budget_tokens":2000},"messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"from chat","signature":"` + UnsignedThinkingSignature + `"}]},` +
		`{"role":"user","content":"go on"},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"chat again","signature":"` + UnsignedThinkingSignature + `"},{"type":"tool_use","id":"t1","name":"Bash","input":{"n":12345678901234567890}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`
	out = string(NativeHistory(APIAnthropic, []byte(claude)))
	var got struct {
		Thinking json.RawMessage `json:"thinking"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, UnsignedThinkingSignature) || len(got.Messages) != 4 {
		t.Fatalf("got %s: want the marked thinking gone, and the turn that was only thinking with it", out)
	}
	if got.Thinking != nil {
		t.Fatalf("thinking still on: Anthropic refuses a tool turn that lost its thinking")
	}
	if !strings.Contains(out, `"input":{"n":12345678901234567890}`) {
		t.Fatalf("got %s: a tool's input must go as the agent wrote it", out)
	}

	signed := `{"model":"claude-opus-5-5","thinking":{"type":"enabled"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"real"},{"type":"tool_use","id":"t","name":"B","input":{}}]}]}`
	if got := string(NativeHistory(APIAnthropic, []byte(signed))); got != signed {
		t.Fatalf("a signed conversation was changed: %s", got)
	}
}
