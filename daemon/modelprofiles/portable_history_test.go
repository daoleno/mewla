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

// anthropicSwitchBody is a Claude Code-shaped request after a provider switch:
// thinking signed by the old upstream, HTML-sensitive text, and non-alphabetical
// key order. A transparent gateway must deliver these exact bytes.
const anthropicSwitchBody = `{"model":"claude-sonnet-4-6","max_tokens":64,"system":[{"type":"text","text":"You are Claude Code"}],"messages":[{"role":"user","content":"<mewla_work_event>{\"a\":1}</mewla_work_event> & go"},{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"sig-A"},{"type":"text","text":"ok"}]},{"role":"user","content":"next"}],"metadata":{"user_id":"u"}}`

func anthropicCaptureUpstream(t *testing.T, sink *[][]byte, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*sink = append(*sink, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
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

func TestAnthropicProviderSwitchForwardsClientBytesUnchanged(t *testing.T) {
	var mu sync.Mutex
	var hitA, hitB [][]byte
	upA := anthropicCaptureUpstream(t, &hitA, &mu)
	upB := anthropicCaptureUpstream(t, &hitB, &mu)

	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "ready", true })
	a := routedClaude(upA.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	state, err := table.BindLaunch("claude", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(table).Handler())
	defer srv.Close()
	root, err := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	postAnthropic(t, root, anthropicSwitchBody)

	b := routedClaude(upB.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	got, err := table.Activate("claude", b, 2, state.Generation, verifiedAuth(b))
	if err != nil {
		t.Fatalf("cross-provider anthropic switch rejected: %v", err)
	}
	if got.Binding.HistoryPortability != "" {
		t.Fatalf("anthropic route must not strip, portability=%q", got.Binding.HistoryPortability)
	}
	postAnthropic(t, root, anthropicSwitchBody)

	mu.Lock()
	defer mu.Unlock()
	if len(hitA) != 1 || len(hitB) != 1 {
		t.Fatalf("hitA=%d hitB=%d", len(hitA), len(hitB))
	}
	for label, got := range map[string][]byte{"A": hitA[0], "B": hitB[0]} {
		if string(got) != anthropicSwitchBody {
			t.Fatalf("upstream %s received rewritten body:\n got %s\nwant %s", label, got, anthropicSwitchBody)
		}
	}
}

func TestAnthropicRouteIgnoresStalePersistedStripFlag(t *testing.T) {
	var mu sync.Mutex
	var hits [][]byte
	up := anthropicCaptureUpstream(t, &hits, &mu)

	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "ready", true })
	a := routedClaude(up.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	state, err := table.BindLaunch("claude", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	// An older daemon persisted the strip flag on this Anthropic route.
	raw, err := EncodeDurableSnapshot(table.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states=%d", len(states))
	}
	states[0].Binding.HistoryPortability = HistoryPortabilityStripOpaque
	restored := NewRouteTable()
	restored.SetLookup(func(string) (string, bool) { return "ready", true })
	if _, err := restored.Restore(states, registerAllow(nil, a)); err != nil {
		t.Fatal(err)
	}
	if got, _ := restored.Get("claude"); got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("precondition: stale flag not restored: %q", got.Binding.HistoryPortability)
	}
	srv := httptest.NewServer(NewRouter(restored).Handler())
	defer srv.Close()
	root, err := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	postAnthropic(t, root, anthropicSwitchBody)

	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 1 || string(hits[0]) != anthropicSwitchBody {
		t.Fatalf("stale strip flag rewrote the body: %q", hits)
	}
}
