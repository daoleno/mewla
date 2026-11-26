package translate

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseBody builds an upstream SSE reply from data lines (and optional names).
func sseBody(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

func upstreamResp(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

// clientEvents parses an SSE reply written to the agent.
func clientEvents(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	r := newSSEReader(strings.NewReader(body))
	for {
		ev, err := r.next()
		if err != nil {
			return out
		}
		var m map[string]any
		if json.Unmarshal(ev.Data, &m) == nil {
			out = append(out, m)
		}
	}
}

func types(events []map[string]any) []string {
	var out []string
	for _, e := range events {
		out = append(out, e["type"].(string))
	}
	return out
}

func TestClaudeCodeOnAChatUpstream(t *testing.T) {
	req := `{"model":"glm-5","max_tokens":1000,"stream":true,
	 "system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.332.0b8; cc_entrypoint=cli; cch=9a8b7;"},{"type":"text","text":"You are Claude Code."}],
	 "tools":[{"name":"Bash","description":"run","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}}}},{"type":"web_search_20250305","name":"web_search"}],
	 "messages":[
	  {"role":"user","content":"list files"},
	  {"role":"assistant","content":[{"type":"thinking","thinking":"use ls","signature":"sig"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"cmd":"ls"}}]},
	  {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a.txt"},{"type":"text","text":"and?"}]}]}`
	x, err := Prepare(APIAnthropic, APIChat, []byte(req), "")
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
		Tools    []map[string]any `json:"tools"`
		Stream   bool             `json:"stream"`
	}
	if err := json.Unmarshal(x.Body, &sent); err != nil {
		t.Fatal(err)
	}
	roles := []string{}
	for _, m := range sent.Messages {
		roles = append(roles, m["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool,user" || len(sent.Tools) != 1 || !sent.Stream {
		t.Fatalf("roles=%v tools=%d stream=%v: want system,user,assistant,tool,user with Bash only (server tools dropped)", roles, len(sent.Tools), sent.Stream)
	}
	if sent.Messages[0]["content"] != "You are Claude Code." {
		t.Fatalf("system = %v: Claude Code's billing line must not reach another API", sent.Messages[0])
	}
	if sent.Messages[2]["reasoning_content"] != "use ls" || sent.Messages[3]["tool_call_id"] != "toolu_1" {
		t.Fatalf("assistant=%v tool=%v: want reasoning back and the call answered", sent.Messages[2], sent.Messages[3])
	}

	upstream := sseBody(
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"hmm"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"Here: "}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"Bash","arguments":"{\"cmd\":"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"Bash","arguments":"{\"cmd\":\"pwd\"}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":60}}}`,
		`data: [DONE]`)
	w := httptest.NewRecorder()
	usage, err := x.Reply(w, upstreamResp(200, "text/event-stream", upstream))
	if err != nil {
		t.Fatal(err)
	}
	events := clientEvents(t, w.Body.String())
	got := strings.Join(types(events), " ")
	want := "message_start content_block_start content_block_delta content_block_delta content_block_stop content_block_start content_block_delta content_block_stop " +
		"content_block_start content_block_delta content_block_stop content_block_start content_block_delta content_block_stop message_delta message_stop"
	if got != want {
		t.Fatalf("events:\n%s\nwant:\n%s", got, want)
	}
	// Interleaved argument pieces of two parallel calls stay with their call.
	if d := events[3]["delta"].(map[string]any); d["signature"] != UnsignedThinkingSignature {
		t.Fatalf("thinking signature = %v: a Chat model's reasoning gets Mewla's own", d)
	}
	if d := events[9]["delta"].(map[string]any); d["partial_json"] != `{"cmd":"ls"}` {
		t.Fatalf("first call args = %v", d["partial_json"])
	}
	if d := events[12]["delta"].(map[string]any); d["partial_json"] != `{"cmd":"pwd"}` {
		t.Fatalf("second call args = %v", d["partial_json"])
	}
	md := events[14]
	if md["delta"].(map[string]any)["stop_reason"] != "tool_use" {
		t.Fatalf("stop = %v", md["delta"])
	}
	u := md["usage"].(map[string]any)
	if u["input_tokens"] != float64(40) || u["cache_read_input_tokens"] != float64(60) || u["output_tokens"] != float64(20) || usage.CacheRead != 60 {
		t.Fatalf("usage = %v (%+v): want cached tokens split out", u, usage)
	}
}

func TestCodexOnAChatUpstream(t *testing.T) {
	req := `{"model":"qwen3","stream":true,"reasoning":{"effort":"high"},
	 "input":[
	  {"type":"message","role":"developer","content":[{"type":"input_text","text":"You are Codex."}]},
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"run it"}]},
	  {"type":"function_call","call_id":"c1","name":"spawn_agent","namespace":"multi_agent_v1","arguments":"{}"},
	  {"type":"function_call","call_id":"c2","name":"exec_command","arguments":"{\"cmd\":\"ls\"}"},
	  {"type":"function_call_output","call_id":"c2","output":"ok"}],
	 "tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}},
	  {"type":"namespace","name":"multi_agent_v1","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object"}}]},
	  {"type":"custom","name":"apply_patch","description":"patch"},{"type":"web_search"}]}`
	x, err := Prepare(APIResponses, APIChat, []byte(req), "")
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
		Tools    []struct {
			Function struct{ Name string } `json:"function"`
		} `json:"tools"`
		Effort string `json:"reasoning_effort"`
	}
	_ = json.Unmarshal(x.Body, &sent)
	names := []string{}
	for _, tool := range sent.Tools {
		names = append(names, tool.Function.Name)
	}
	if strings.Join(names, ",") != "exec_command,multi_agent_v1__spawn_agent,apply_patch" || sent.Effort != "high" {
		t.Fatalf("tools=%v effort=%q", names, sent.Effort)
	}
	// c1 was never answered: the conversation is mended before Chat sees it.
	var answered []string
	for _, m := range sent.Messages {
		if m["role"] == "tool" {
			answered = append(answered, m["tool_call_id"].(string))
		}
	}
	if strings.Join(answered, ",") != "c1,c2" || sent.Messages[0]["content"] != "You are Codex." {
		t.Fatalf("tool answers=%v system=%v", answered, sent.Messages[0])
	}

	upstream := sseBody(
		`data: {"choices":[{"index":0,"delta":{"content":"Doing it."}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_x","function":{"name":"multi_agent_v1__spawn_agent","arguments":"{\"message\":\"hi\"}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_y","function":{"name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\"}"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
		`data: [DONE]`)
	w := httptest.NewRecorder()
	if _, err := x.Reply(w, upstreamResp(200, "text/event-stream", upstream)); err != nil {
		t.Fatal(err)
	}
	events := clientEvents(t, w.Body.String())
	var done []map[string]any
	var completed map[string]any
	for _, e := range events {
		switch e["type"] {
		case "response.output_item.done":
			done = append(done, e["item"].(map[string]any))
		case "response.completed":
			completed = e["response"].(map[string]any)
		}
	}
	if len(done) != 3 || done[0]["type"] != "message" {
		t.Fatalf("items = %v", done)
	}
	if done[1]["type"] != "function_call" || done[1]["name"] != "spawn_agent" || done[1]["namespace"] != "multi_agent_v1" {
		t.Fatalf("namespaced call = %v: want its namespace back", done[1])
	}
	if done[2]["type"] != "custom_tool_call" || done[2]["input"] != "*** Begin Patch" {
		t.Fatalf("freeform call = %v: want custom_tool_call with plain input", done[2])
	}
	if completed == nil || completed["usage"].(map[string]any)["total_tokens"] != float64(15) {
		t.Fatalf("completed = %v", completed)
	}
}

func TestCodexOnAnAnthropicUpstreamKeepsThinkingAcrossTurns(t *testing.T) {
	first := `{"model":"claude-sonnet-5","stream":true,"reasoning":{"effort":"medium"},"input":[{"type":"message","role":"user","content":"think"}],"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}}]}`
	x, err := Prepare(APIResponses, APIAnthropic, []byte(first), "")
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal(x.Body, &sent)
	if sent["thinking"].(map[string]any)["budget_tokens"] != float64(10000) || sent["max_tokens"].(float64) <= 10000 {
		t.Fatalf("thinking=%v max_tokens=%v", sent["thinking"], sent["max_tokens"])
	}
	tools := sent["tools"].([]any)
	if _, ok := tools[len(tools)-1].(map[string]any)["cache_control"]; !ok {
		t.Fatalf("no cache breakpoint on the tools: every turn would be billed in full")
	}
	upstream := sseBody(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":100}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG=="}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_9","name":"exec_command"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"ls\"}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`,
		`data: {"type":"message_stop"}`)
	w := httptest.NewRecorder()
	if _, err := x.Reply(w, upstreamResp(200, "text/event-stream", upstream)); err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	for _, e := range clientEvents(t, w.Body.String()) {
		if e["type"] == "response.output_item.done" {
			items = append(items, e["item"].(map[string]any))
		}
	}
	if len(items) != 2 || items[0]["type"] != "reasoning" || !strings.HasPrefix(items[0]["encrypted_content"].(string), anthropicSealPrefix) {
		t.Fatalf("items = %v: want a reasoning item carrying the sealed thinking", items)
	}
	// Codex sends the items back next turn; the thinking goes back signed.
	next, _ := json.Marshal(map[string]any{
		"model": "claude-sonnet-5", "stream": true, "reasoning": map[string]string{"effort": "medium"},
		"tools": []any{map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]string{"type": "object"}}},
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "think"},
			items[0],
			map[string]any{"type": "function_call", "call_id": items[1]["call_id"], "name": "exec_command", "arguments": items[1]["arguments"]},
			map[string]any{"type": "function_call_output", "call_id": items[1]["call_id"], "output": "a.txt"},
		},
	})
	x2, err := Prepare(APIResponses, APIAnthropic, next, "")
	if err != nil {
		t.Fatal(err)
	}
	var sent2 struct {
		Thinking map[string]any `json:"thinking"`
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(x2.Body, &sent2)
	asst := sent2.Messages[1]
	if sent2.Thinking == nil || asst.Content[0]["type"] != "thinking" || asst.Content[0]["signature"] != "SIG==" || asst.Content[0]["thinking"] != "plan" || asst.Content[1]["type"] != "tool_use" {
		t.Fatalf("thinking=%v assistant=%v: want the signed thinking block before the call, thinking still on", sent2.Thinking, asst.Content)
	}
}

func TestErrorsSpeakTheAgentsLanguage(t *testing.T) {
	overflow := `{"error":{"message":"This model's maximum context length is 128000 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`
	x, _ := Prepare(APIAnthropic, APIChat, []byte(`{"model":"m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`), "")
	w := httptest.NewRecorder()
	_, _ = x.Reply(w, upstreamResp(400, "application/json", overflow))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "prompt is too long") || !strings.Contains(w.Body.String(), `"type":"error"`) {
		t.Fatalf("status=%d body=%s: Claude Code compacts on 'prompt is too long'", w.Code, w.Body.String())
	}
	x, _ = Prepare(APIResponses, APIAnthropic, []byte(`{"model":"m","input":"hi"}`), "")
	w = httptest.NewRecorder()
	_, _ = x.Reply(w, upstreamResp(400, "application/json", `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens > 200000 maximum"}}`))
	if !strings.Contains(w.Body.String(), "context_length_exceeded") {
		t.Fatalf("body=%s: Codex compacts on context_length_exceeded", w.Body.String())
	}
	x, _ = Prepare(APIAnthropic, APIChat, []byte(`{"model":"m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`), "")
	w = httptest.NewRecorder()
	_, _ = x.Reply(w, upstreamResp(429, "application/json", `{"error":{"message":"slow down"}}`))
	if w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limit_error") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAStreamCutShortIsAnError(t *testing.T) {
	x, _ := Prepare(APIResponses, APIChat, []byte(`{"model":"m","stream":true,"input":"hi"}`), "")
	w := httptest.NewRecorder()
	_, err := x.Reply(w, upstreamResp(200, "text/event-stream", sseBody(`data: {"choices":[{"index":0,"delta":{"content":"par"}}]}`)))
	if err == nil || !strings.Contains(w.Body.String(), "response.failed") {
		t.Fatalf("err=%v body=%s: a reply cut off must not look complete", err, w.Body.String())
	}
}

func TestWholeRepliesForNonStreamingAgents(t *testing.T) {
	x, _ := Prepare(APIAnthropic, APIChat, []byte(`{"model":"m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`), "")
	w := httptest.NewRecorder()
	_, _ = x.Reply(w, upstreamResp(200, "application/json", `{"choices":[{"index":0,"message":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	var msg map[string]any
	if json.Unmarshal(w.Body.Bytes(), &msg) != nil || msg["type"] != "message" || msg["stop_reason"] != "end_turn" {
		t.Fatalf("body=%s", w.Body.String())
	}
	if c := msg["content"].([]any)[0].(map[string]any); c["text"] != "hello" {
		t.Fatalf("content=%v", c)
	}
}

func TestCountTokensEstimate(t *testing.T) {
	out, err := CountTokens([]byte(`{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("abcd", 1000) + `"}]}`))
	var got map[string]int
	if err != nil || json.Unmarshal(out, &got) != nil || got["input_tokens"] < 900 || got["input_tokens"] > 1200 {
		t.Fatalf("estimate=%s err=%v, want about 1000", out, err)
	}
}

func TestEndpointServed(t *testing.T) {
	for _, c := range []struct {
		status       int
		body         string
		served, sure bool
	}{
		{400, `{"error":{"message":"messages: field required"}}`, true, true},
		{422, `{}`, true, true},
		{401, `{}`, true, true},
		{404, `{"error":"Invalid URL (POST /v1/responses)"}`, false, true},
		{405, ``, false, true},
		{502, ``, false, false},
	} {
		served, sure := EndpointServed(c.status, []byte(c.body))
		if served != c.served || sure != c.sure {
			t.Errorf("%d %s: got %v %v", c.status, c.body, served, sure)
		}
	}
}

// 11-27 live: claude-sonnet-5 answers a thinking block with no text, only a
// signature. Unless that signature reaches Codex, the next turn of a tool
// loop cannot send the thinking back, and thinking is off from then on.
func TestCodexKeepsASignatureWithoutThinkingText(t *testing.T) {
	x, _ := Prepare(APIResponses, APIAnthropic, []byte(`{"model":"claude-sonnet-5","stream":true,"reasoning":{"effort":"high"},"input":"hi"}`), "")
	upstream := sseBody(
		`data: {"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"ONLY-SIG"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"exec_command"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`data: {"type":"message_stop"}`)
	w := httptest.NewRecorder()
	if _, err := x.Reply(w, upstreamResp(200, "text/event-stream", upstream)); err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	for _, e := range clientEvents(t, w.Body.String()) {
		if e["type"] == "response.output_item.done" {
			items = append(items, e["item"].(map[string]any))
		}
	}
	if len(items) != 2 || items[0]["type"] != "reasoning" {
		t.Fatalf("items = %v: want the signed reasoning item before the call", items)
	}
	sig, part, ok := unsealAnthropicThinking(items[0]["encrypted_content"].(string))
	if !ok || sig != "ONLY-SIG" || part.Text != "" {
		t.Fatalf("sealed = %q %+v %v", sig, part, ok)
	}
}

// 11-27 Mac: Claude Code 2.1.236 sends its turn's context as a
// {"role":"system"} message after the person's prompt. Told as a user
// message, it became the last thing the model was asked, and the model
// answered "What would you like me to work on?" instead of doing the task.
func TestASystemMessageInTheConversationStaysOne(t *testing.T) {
	req := `{"model":"glm-5","max_tokens":100,"stream":true,
	 "system":[{"type":"text","text":"You are Claude Code."}],
	 "messages":[
	  {"role":"user","content":[{"type":"text","text":"<system-reminder>ctx</system-reminder>"},{"type":"text","text":"Create hello.txt"}]},
	  {"role":"system","content":[{"type":"text","text":"Available agent types: …","cache_control":{"type":"ephemeral"}}]}]}`
	x, err := Prepare(APIAnthropic, APIChat, []byte(req), "")
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.Unmarshal(x.Body, &sent)
	var roles []string
	for _, m := range sent.Messages {
		roles = append(roles, m["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,system" || sent.Messages[2]["content"] != "Available agent types: …" {
		t.Fatalf("messages = %v: want the turn's context kept as a system message after the prompt", sent.Messages)
	}

	// While a call waits for its answer, the system message goes after it.
	held := []Message{
		{Role: RoleAssistant, Parts: []Part{{Kind: PartToolCall, CallID: "c1", Name: "Bash", Args: "{}"}}},
		{Role: RoleSystem, Parts: []Part{{Kind: PartText, Text: "note"}}},
		{Role: RoleUser, Parts: []Part{{Kind: PartToolResult, CallID: "c1", Result: []Part{{Kind: PartText, Text: "ok"}}}}},
	}
	var order []string
	for _, m := range pairToolCalls(held) {
		order = append(order, m.Role)
	}
	if strings.Join(order, ",") != "assistant,user,system" {
		t.Fatalf("order = %v: Chat wants a call's answer right after it", order)
	}

	// Toward Anthropic it is the user's text, in place.
	x, err = Prepare(APIResponses, APIAnthropic, []byte(`{"model":"claude-sonnet-5","input":[
	  {"type":"message","role":"user","content":"hi"},
	  {"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
	  {"type":"message","role":"developer","content":"be brief"}]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	var anth struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(x.Body, &anth)
	if len(anth.Messages) != 3 || anth.Messages[2].Role != "user" {
		t.Fatalf("anthropic messages = %+v", anth.Messages)
	}
}
