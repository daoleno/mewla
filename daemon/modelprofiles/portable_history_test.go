package modelprofiles

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNormalizeResponsesPortableHistoryStripsOpaqueKeepsTextTools(t *testing.T) {
	in := []byte(`{
	  "model":"cli",
	  "previous_response_id":"resp_old",
	  "prompt_cache_key":"cache-1",
	  "input":[
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t1"}},
	    {"type":"reasoning","id":"rsn_1","encrypted_content":"opaque-blob"},
	    {"type":"item_reference","id":"item_1"},
	    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"token-1"},{"type":"reasoning","encrypted_content":"x"},{"type":"refusal","refusal":"nope"}]},
	    {"type":"function_call","name":"Bash","call_id":"c1","arguments":"{\"command\":\"echo hi\"}","provider_secret":"leak"},
	    {"type":"function_call_output","call_id":"c1","output":"hi","provider_secret":"leak"}
	  ]
	}`)
	out, deg, err := normalizePortableHistoryBody(RouteProtocolResponses, in)
	if err != nil || deg != HistoryDegradationStripOpaque {
		t.Fatalf("err=%v deg=%q", err, deg)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["previous_response_id"]; ok {
		t.Fatalf("previous_response_id leaked: %#v", obj)
	}
	if obj["prompt_cache_key"] != "cache-1" {
		t.Fatalf("prompt_cache_key=%v", obj["prompt_cache_key"])
	}
	raw, _ := json.Marshal(obj["input"])
	s := string(raw)
	if strings.Contains(s, "previous_response_id") || strings.Contains(s, "encrypted_content") || strings.Contains(s, "item_reference") {
		t.Fatalf("opaque leaked: %s", s)
	}
	if strings.Contains(s, "provider_secret") || strings.Contains(s, "internal_chat_message_metadata") {
		t.Fatalf("unknown provider fields leaked: %s", s)
	}
	if !strings.Contains(s, `"alpha"`) || !strings.Contains(s, `"token-1"`) || !strings.Contains(s, `"function_call"`) {
		t.Fatalf("portable text/tools missing: %s", s)
	}
	if !strings.Contains(s, `"refusal"`) || !strings.Contains(s, `"nope"`) {
		t.Fatalf("visible refusal dropped: %s", s)
	}
}

func TestNormalizeResponsesPortableHistoryPreservesUnknownType(t *testing.T) {
	in := []byte(`{"input":[{"type":"vendor_secret_item","payload":"x"}]}`)
	out, _, err := normalizePortableHistoryBody(RouteProtocolResponses, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "vendor_secret_item") || !strings.Contains(string(out), "payload") {
		t.Fatalf("unknown item was lost: %s", out)
	}
}

func TestPortableHistoryStickySurvivesDurableRestore(t *testing.T) {
	table := NewRouteTable()
	a := routedCodex("http://127.0.0.1:9/v1", "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("sticky", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := table.MarkHistoryMayContainOpaque(state.Binding.RouteID); err != nil {
		t.Fatal(err)
	}
	b := routedCodex("http://127.0.0.1:9/v1", "gpt-5.5", "gpt-5.5")
	got, err := table.Activate("sticky", b, 2, state.Generation, verifiedAuth(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", got.Binding.HistoryPortability)
	}
	raw, err := EncodeDurableSnapshot(table.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"history_portability"`)) {
		t.Fatalf("portability not durable: %s", raw)
	}
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewRouteTable()
	if _, err := restored.Restore(states, registerAllow(registerAllow(nil, a), b)); err != nil {
		t.Fatal(err)
	}
	again, ok := restored.Get("sticky")
	if !ok || again.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("sticky portability lost after restore: %#v", again.Binding)
	}
	if again.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route id changed on restore")
	}
}

func TestPortableHistoryHotSwitchRouterAThenB(t *testing.T) {
	var mu sync.Mutex
	var hitA, hitB []string
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hitA = append(hitA, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_a","object":"response","status":"completed","output":[]}`))
	}))
	defer upA.Close()
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hitB = append(hitB, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_b","object":"response","status":"completed","output":[]}`))
	}))
	defer upB.Close()

	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "ready", true })
	a := routedCodex(upA.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("sess", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	body1 := `{"model":"gpt-5.6-sol","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"}]}]}`
	resp, err := http.Post(base+"/responses", "application/json", strings.NewReader(body1))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	b := routedCodex(upB.URL, "gpt-5.5", "gpt-5.5")
	got, err := table.Activate("sess", b, 2, 1, verifiedAuth(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", got.Binding.HistoryPortability)
	}
	if got.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route id changed: %q -> %q", state.Binding.RouteID, got.Binding.RouteID)
	}

	body2 := `{
	  "model":"gpt-5.5",
	  "previous_response_id":"resp_a",
	  "input":[
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"}]},
	    {"type":"reasoning","encrypted_content":"nope"},
	    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"token-1"}]},
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"beta"}]}
	  ]
	}`
	resp, err = http.Post(base+"/responses", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	rawB, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, rawB)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hitA) != 1 || len(hitB) != 1 {
		t.Fatalf("hitA=%d hitB=%d", len(hitA), len(hitB))
	}
	if !strings.Contains(hitA[0], `"gpt-5.6-sol"`) && !strings.Contains(hitA[0], "alpha") {
		t.Fatalf("gateway A body=%s", hitA[0])
	}
	if strings.Contains(hitB[0], "previous_response_id") || strings.Contains(hitB[0], "encrypted_content") {
		t.Fatalf("gateway B received opaque: %s", hitB[0])
	}
	if !strings.Contains(hitB[0], `"gpt-5.5"`) || !strings.Contains(hitB[0], "token-1") || !strings.Contains(hitB[0], "beta") {
		t.Fatalf("gateway B missing portable continuity: %s", hitB[0])
	}
}

func TestPortableHistoryActivateWhileInFlightSwapsRouteAndMarksOpaque(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-hold
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"r","status":"completed","output":[]}`))
	}))
	defer up.Close()
	table := NewRouteTable()
	a := routedCodex(up.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Post(base+"/responses", "application/json", strings.NewReader(`{"model":"cli","input":[]}`))
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	// The in-flight request runs against its immutable old snapshot; a
	// cross-domain activation must still swap the route so later requests use
	// the new binding (never a busy/read-only Session).
	b := routedCodex(up.URL, "gpt-5.5", "gpt-5.5")
	got, err := table.Activate("s", b, 2, 1, verifiedAuth(b))
	if err != nil {
		t.Fatalf("activate while in-flight err=%v", err)
	}
	if got.Binding.UpstreamModel != "gpt-5.5" || got.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route must swap atomically on the same route: %#v", got.Binding)
	}
	close(hold)
	<-done
	// The 2xx from the old snapshot still marks the Session's history opaque:
	// the CLI retained that provider's opaque state, so cross-domain guards
	// keep applying to the next switch.
	got, _ = table.Get("s")
	if got.Binding.HistoryState != HistoryStateMayContainOpaque {
		t.Fatalf("2xx should mark opaque: %q", got.Binding.HistoryState)
	}
}

// anthropicBody builds a Claude Code-shaped request whose assistant turn
// carries the given content parts. HTML-sensitive text and non-alphabetical key
// order catch any re-encoding of bytes the router must not touch.
func anthropicBody(assistantParts string) string {
	return `{"model":"claude-sonnet-4-6","max_tokens":64,"system":[{"type":"text","text":"You are Claude Code"}],"messages":[{"role":"user","content":"<mewla_work_event>{\"a\":1}</mewla_work_event> & go"},{"role":"assistant","content":[` + assistantParts + `]},{"role":"user","content":"next"}],"metadata":{"user_id":"u"}}`
}

const (
	thinkingA = `{"type":"thinking","thinking":"plan","signature":"sig-A"}`
	thinkingB = `{"type":"thinking","thinking":"plan","signature":"sig-B1sig-B2"}`
	thinkingJ = `{"type":"thinking","thinking":"plan","signature":"sig-J"}`
	textOK    = `{"type":"text","text":"ok"}`
)

type anthropicCapture struct {
	mu    sync.Mutex
	hits  [][]byte
	reply func(w http.ResponseWriter)
}

func (c *anthropicCapture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.hits = append(c.hits, body)
		reply := c.reply
		c.mu.Unlock()
		if reply != nil {
			reply(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *anthropicCapture) last(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.hits) == 0 {
		t.Fatal("upstream not hit")
	}
	return string(c.hits[len(c.hits)-1])
}

// sseThinkingReply streams a thinking block whose signature arrives in two
// signature_delta events, as the Messages API may split it.
func sseThinkingReply(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []string{
		`{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[]}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-B1"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-B2"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	}
	for _, ev := range events {
		_, _ = io.WriteString(w, "event: x\ndata: "+ev+"\n\n")
	}
}

func postAnthropic(t *testing.T, rootURL, body string) {
	t.Helper()
	resp, err := http.Post(rootURL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
}

// switchedAnthropicRoute launches a Claude route on upstream A, sends one turn
// so history may hold A-signed thinking, then switches the Session to B.
func switchedAnthropicRoute(t *testing.T, a, b *anthropicCapture) (root string) {
	t.Helper()
	upA, upB := a.server(t), b.server(t)
	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "ready", true })
	pa := routedClaude(upA.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	state, err := table.BindLaunch("claude", pa, 1, verifiedAuth(pa))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(table).Handler())
	t.Cleanup(srv.Close)
	root, err = LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	before := anthropicBody(thinkingA + "," + textOK)
	postAnthropic(t, root, before)
	if got := a.last(t); got != before {
		t.Fatalf("unswitched route rewrote the body:\n got %s\nwant %s", got, before)
	}
	pb := routedClaude(upB.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	got, err := table.Activate("claude", pb, 2, state.Generation, verifiedAuth(pb))
	if err != nil {
		t.Fatalf("cross-provider anthropic switch rejected: %v", err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("switched anthropic route portability=%q", got.Binding.HistoryPortability)
	}
	return root
}

func TestAnthropicSwitchDropsOnlyThinkingTheNewUpstreamDidNotIssue(t *testing.T) {
	a := &anthropicCapture{}
	b := &anthropicCapture{reply: sseThinkingReply}
	root := switchedAnthropicRoute(t, a, b)

	// First request after the switch: A's thinking is foreign and dropped;
	// every other byte is the client's.
	postAnthropic(t, root, anthropicBody(thinkingA+","+textOK))
	if got, want := b.last(t), anthropicBody(textOK); got != want {
		t.Fatalf("B body:\n got %s\nwant %s", got, want)
	}
	// B issued sig-B1sig-B2 in its streamed reply; it is kept, A's still goes.
	postAnthropic(t, root, anthropicBody(thinkingA+","+thinkingB+","+textOK))
	if got, want := b.last(t), anthropicBody(thinkingB+","+textOK); got != want {
		t.Fatalf("B body:\n got %s\nwant %s", got, want)
	}
	// A request with only B-issued thinking is forwarded byte-for-byte.
	same := anthropicBody(thinkingB + "," + textOK)
	postAnthropic(t, root, same)
	if got := b.last(t); got != same {
		t.Fatalf("B body rewritten:\n got %s\nwant %s", got, same)
	}
}

func TestAnthropicSwitchLearnsThinkingFromJSONReplies(t *testing.T) {
	a := &anthropicCapture{}
	b := &anthropicCapture{reply: func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg","type":"message","role":"assistant","content":[` + thinkingJ + `,{"type":"text","text":"ok"}]}`))
	}}
	root := switchedAnthropicRoute(t, a, b)
	postAnthropic(t, root, anthropicBody(textOK))
	body := anthropicBody(thinkingJ + "," + textOK)
	postAnthropic(t, root, body)
	if got := b.last(t); got != body {
		t.Fatalf("B-issued JSON thinking was dropped:\n got %s\nwant %s", got, body)
	}
}

func TestAnthropicSwitchKeepsThinkingOnlyTurnNonEmpty(t *testing.T) {
	a := &anthropicCapture{}
	b := &anthropicCapture{}
	root := switchedAnthropicRoute(t, a, b)
	postAnthropic(t, root, anthropicBody(thinkingA))
	want := anthropicBody(`{"text":"` + anthropicThinkingOmittedText + `","type":"text"}`)
	if got := b.last(t); got != want {
		t.Fatalf("B body:\n got %s\nwant %s", got, want)
	}
}

func TestDropForeignAnthropicThinkingLeavesCleanBodyIdentical(t *testing.T) {
	body := []byte(anthropicBody(textOK))
	out, changed, err := dropForeignAnthropicThinking(body, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if changed || !bytes.Equal(out, body) {
		t.Fatalf("clean body changed: %s", out)
	}
	if _, _, err := dropForeignAnthropicThinking([]byte(`{"messages":{}}`), func(string) bool { return false }); err == nil {
		t.Fatal("non-array messages must fail closed")
	}
}
