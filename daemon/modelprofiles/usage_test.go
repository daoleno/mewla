package modelprofiles

import (
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// replay writes a reply through a recorder in small pieces, as a stream
// arrives, and returns what was kept.
func replay(t *testing.T, contentType string, status int, reply string) ConnectionUsage {
	t.Helper()
	log := newUsageLog("")
	rec := newUsageRecorder(httptest.NewRecorder(), log, ClientClaude, "conn-a")
	rec.asked = "asked-model"
	rec.Header().Set("Content-Type", contentType)
	rec.WriteHeader(status)
	for len(reply) > 0 {
		n := min(7, len(reply))
		if _, err := rec.Write([]byte(reply[:n])); err != nil {
			t.Fatal(err)
		}
		reply = reply[n:]
	}
	rec.finish()
	rec.finish() // once only
	return log.of("conn-a")
}

func TestUsageIsReadFromEachAgentsReply(t *testing.T) {
	anthropicStream := "event: message_start\n" +
		`data: {"type":"message_start","message":{"model":"glm-5","usage":{"input_tokens":12,"cache_read_input_tokens":800,"cache_creation_input_tokens":40,"output_tokens":1}}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"the \"usage\" word"}}` + "\n\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":57}}` + "\n\n" +
		`data: {"type":"message_stop"}` + "\n\n"
	responsesStream := `data: {"type":"response.created","response":{"model":"gpt-5.5","usage":null}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","delta":"hi"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"model":"gpt-5.5","usage":{"input_tokens":900,"input_tokens_details":{"cached_tokens":600},"output_tokens":30}}}` + "\n\n"
	for _, c := range []struct {
		name, contentType string
		status            int
		reply             string
		want              UsageRecord
		wantErr           string
	}{
		{"anthropic stream", "text/event-stream", 200, anthropicStream, UsageRecord{Model: "glm-5", Input: 12, CacheRead: 800, CacheWrite: 40, Output: 57}, ""},
		{"responses stream", "text/event-stream; charset=utf-8", 200, responsesStream, UsageRecord{Model: "gpt-5.5", Input: 300, CacheRead: 600, Output: 30}, ""},
		{"anthropic json", "application/json", 200, `{"model":"kimi-k2","usage":{"input_tokens":5,"output_tokens":9}}`, UsageRecord{Model: "kimi-k2", Input: 5, Output: 9}, ""},
		{"no model named", "application/json", 200, `{"usage":{"input_tokens":5,"output_tokens":9}}`, UsageRecord{Model: "asked-model", Input: 5, Output: 9}, ""},
		{"refused", "application/json", 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key sk-ant-api03-abcdefghijklmnop"}}`, UsageRecord{}, "invalid x-api-key sk-…"},
		{"no words", "text/plain", 502, `Bad Gateway`, UsageRecord{}, "Bad Gateway"},
		{"failed mid-stream", "text/event-stream", 200, `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` + "\n\n", UsageRecord{}, "Overloaded"},
		{"responses failed", "text/event-stream", 200, `data: {"type":"response.failed","response":{"error":{"code":"server_error","message":"The model broke"}}}` + "\n\n", UsageRecord{}, "The model broke"},
		{"responses error event", "text/event-stream", 200, `data: {"type":"error","code":"rate_limit_exceeded","message":"Slow down"}` + "\n\n", UsageRecord{}, "Slow down"},
	} {
		got := replay(t, c.contentType, c.status, c.reply)
		if c.wantErr != "" {
			if len(got.Errors) != 1 || len(got.Requests) != 0 || got.Errors[0].Message != c.wantErr || got.Errors[0].Agent != ClientClaude {
				t.Errorf("%s: %+v", c.name, got)
			}
			continue
		}
		if len(got.Requests) != 1 || len(got.Errors) != 0 {
			t.Errorf("%s: %+v", c.name, got)
			continue
		}
		r := got.Requests[0]
		if r.Model != c.want.Model || r.Input != c.want.Input || r.CacheRead != c.want.CacheRead || r.CacheWrite != c.want.CacheWrite || r.Output != c.want.Output {
			t.Errorf("%s: got %+v, want %+v", c.name, r, c.want)
		}
	}
}

func TestUsageLogPricesKeepsAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), providerUsageFile)
	log := newUsageLog(path)
	rate := func(v float64) *float64 { return &v }
	log.prices = func(conn, model string) (usagePrices, bool) {
		if model != "priced" {
			return usagePrices{}, false
		}
		return usagePrices{input: rate(3), output: rate(15), cacheRead: rate(0.3)}, true
	}
	at := time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local)
	// Cache writes have no rate of their own here: they cost as input.
	log.record("A", UsageRecord{At: at, Model: "priced", Input: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000, Output: 100_000})
	log.record("a", UsageRecord{At: at, Model: "unknown", Input: 10, Output: 1})
	got := log.of("a")
	if len(got.Requests) != 2 || got.Requests[0].Model != "unknown" || got.Requests[0].Priced {
		t.Fatalf("requests %+v", got.Requests)
	}
	if want := 3 + 0.3 + 3 + 1.5; !got.Requests[1].Priced || math.Abs(got.Requests[1].CostUSD-want) > 1e-9 {
		t.Fatalf("cost %v, want %v", got.Requests[1].CostUSD, want)
	}
	day := got.Days["2026-03-04"]
	if day.Requests != 2 || day.Unpriced != 1 || day.Input != 1_000_010 || day.Output != 100_001 {
		t.Fatalf("day %+v", day)
	}
	for i := 0; i < usageKeepRequests+5; i++ {
		log.record("a", UsageRecord{At: at.Add(time.Duration(i) * 24 * time.Hour), Model: "unknown", Input: int64(i)})
		log.fail("a", UsageError{At: at, Status: 503, Message: strings.Repeat("x", 400)})
	}
	got = log.of("a")
	if len(got.Requests) != usageKeepRequests || got.Requests[0].Input != usageKeepRequests+4 || len(got.Errors) != usageKeepErrors || len(got.Days) != usageKeepDays {
		t.Fatalf("kept %d requests (newest %d), %d errors, %d days", len(got.Requests), got.Requests[0].Input, len(got.Errors), len(got.Days))
	}
	if n := len([]rune(got.Errors[0].Message)); n != usageMessageMax+1 {
		t.Fatalf("message kept at %d runes", n)
	}
	// What was kept is there after a restart; a deleted connection's is not.
	if err := log.save(); err != nil {
		t.Fatal(err)
	}
	again := newUsageLog(path)
	if got := again.of("a"); len(got.Requests) != usageKeepRequests || len(got.Errors) != usageKeepErrors {
		t.Fatalf("after load: %d requests, %d errors", len(got.Requests), len(got.Errors))
	}
	again.forget("a")
	if err := again.save(); err != nil {
		t.Fatal(err)
	}
	if got := newUsageLog(path).of("a"); len(got.Requests) != 0 || len(got.Errors) != 0 {
		t.Fatalf("forgotten usage came back: %+v", got)
	}
}

// waitUsage waits for a connection's usage to show n requests and m errors:
// a reply's last byte can reach the agent before its handler is done.
func waitUsage(t *testing.T, owner *Owner, id string, n, m int) ConnectionUsage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := owner.ProviderUsage(id)
		if err != nil {
			t.Fatal(err)
		}
		if (len(got.Requests) == n && len(got.Errors) == m) || time.Now().After(deadline) {
			if len(got.Requests) != n || len(got.Errors) != m {
				t.Fatalf("connection %s: %d requests, %d errors; want %d, %d: %+v", id, len(got.Requests), len(got.Errors), n, m, got)
			}
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUsageIsKeptForTheConnectionThatAnswered(t *testing.T) {
	stubRetrySleep(t)
	// A Claude Session on a Chat Completions connection: translated, so the
	// usage is read from the reply Claude Code gets.
	chat := &oneAPIUpstream{path: "/v1/chat/completions", models: `{"data":[{"id":"glm-5"}]}`, reply: chatReply("hello")}
	csrv := httptest.NewServer(chat)
	defer csrv.Close()
	owner := detectingOwner(t)
	conn := addConnection(t, owner, ClientClaude, "Chat", csrv.URL+"/v1", "glm-5")
	plan, err := owner.PrepareLaunchModel(ExecutorClaude, "", "glm-5", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "usage-fixture"); err != nil {
		t.Fatal(err)
	}
	status, body := post(t, plan.Env[EnvAnthropicBaseURL]+"/v1/messages", `{"model":"glm-5","max_tokens":20,"stream":true,"messages":[{"role":"user","content":"ping"}]}`, nil)
	if status != 200 || !strings.Contains(body, "hello") {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// Counting tokens is not a model request.
	_, _ = post(t, plan.Env[EnvAnthropicBaseURL]+"/v1/messages/count_tokens", `{"model":"glm-5","messages":[{"role":"user","content":"ping"}]}`, nil)
	got := waitUsage(t, owner, conn.ID, 1, 0)
	if r := got.Requests[0]; r.Agent != ClientClaude || r.Model != "glm-5" || r.Input != 3 || r.Output != 1 {
		t.Fatalf("request %+v", r)
	}

	// Codex on the gateway, its connection unreachable: the fallback answers
	// and keeps the request; the connection keeps its failure.
	anth := &oneAPIUpstream{
		path:   "/v1/messages",
		models: `{"data":[{"id":"claude-sonnet-5"}]}`,
		reply: "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4,\"cache_read_input_tokens\":10}}}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"spare\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	}
	asrv := httptest.NewServer(anth)
	defer asrv.Close()
	fb := addConnection(t, owner, ClientCodex, "Anthropic Spare", asrv.URL+"/v1", "claude-sonnet-5")
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	cat, err := owner.UpsertProviderConnection(ProviderConnectionInput{Name: "Main", Client: ClientCodex, PresetID: ProviderPresetCustom, BaseURL: deadURL + "/v1", ModelID: "gpt-5.5", Advanced: true}, "fixture-key-not-real", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	var main ProviderConnection
	for _, c := range cat.Connections {
		if c.Name == "Main" {
			main = c
		}
	}
	if _, err := owner.SetProviderConnection(ClientCodex, main.ID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderFallback(main.ID, fb.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3425/v1/responses",
		strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}]}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	owner.Gateway().ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "spare") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	served := waitUsage(t, owner, fb.ID, 1, 0)
	if r := served.Requests[0]; r.Agent != ClientCodex || r.Model != "claude-sonnet-5" || r.Input != 4 || r.CacheRead != 10 || r.Output != 2 {
		t.Fatalf("fallback kept %+v", r)
	}
	failed := waitUsage(t, owner, main.ID, 0, 1)
	if e := failed.Errors[0]; e.Agent != ClientCodex || e.Model != "gpt-5.5" || e.Message == "" {
		t.Fatalf("connection kept %+v", e)
	}
	if _, err := owner.ProviderUsage("no-such-connection"); err == nil {
		t.Fatal("usage of a connection that is not there")
	}
}

func TestUsageOfACodexWebSocketTurn(t *testing.T) {
	completed := `{"type":"response.completed","response":{"model":"gpt-5.6-sol","usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":100},"output_tokens":7}}}`
	upstream := newWSUpstreamSimulator(t, []string{wsEventCreated, completed})
	log := newUsageLog("")
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayUsage(log))
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	g.SetUpstream(gatewayUpstreamFor(upstream.server.URL))
	client, status := wsClientDial(t, "http://"+g.ActualAddr(), "/v1/responses", map[string]string{"OpenAI-Beta": "responses_websockets=2026-02-06"})
	if status != 101 {
		t.Fatalf("handshake %d", status)
	}
	for turn := 1; turn <= 2; turn++ {
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":[]}`)); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, _, err := client.ReadMessage(); err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(log.of("conn-test").Requests) < turn && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	got := log.of("conn-test")
	if len(got.Requests) != 2 || len(got.Errors) != 0 {
		t.Fatalf("%+v", got)
	}
	if r := got.Requests[0]; r.Agent != ClientCodex || r.Model != "gpt-5.6-sol" || r.Input != 20 || r.CacheRead != 100 || r.Output != 7 {
		t.Fatalf("turn %+v", r)
	}
}
