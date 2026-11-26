package modelprofiles

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// oneAPIUpstream speaks a single API at path: every other POST is 404, as a
// real endpoint that does not serve it answers. It lists models and keeps
// what each request to path carried.
type oneAPIUpstream struct {
	path   string
	models string
	reply  string

	mu     sync.Mutex
	bodies []string
	header []http.Header
	other  []string
}

func (u *oneAPIUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
		_, _ = io.WriteString(w, u.models)
		return
	}
	body, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	defer u.mu.Unlock()
	if r.URL.Path != u.path {
		u.other = append(u.other, r.URL.Path)
		http.Error(w, `{"error":{"message":"Invalid URL"}}`, http.StatusNotFound)
		return
	}
	if strings.TrimSpace(string(body)) == "{}" {
		http.Error(w, `{"error":{"message":"model: field required"}}`, http.StatusBadRequest)
		return
	}
	u.bodies = append(u.bodies, string(body))
	u.header = append(u.header, r.Header.Clone())
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, u.reply)
}

func (u *oneAPIUpstream) last() (string, http.Header) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.bodies) == 0 {
		return "", nil
	}
	return u.bodies[len(u.bodies)-1], u.header[len(u.header)-1]
}

// detectingOwner is a daemon's owner with its machine gateway, finding a
// custom endpoint's API when it is saved as the daemon does.
func detectingOwner(t *testing.T) *Owner {
	t.Helper()
	root := t.TempDir()
	creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:    filepath.Join(root, "model-profiles.toml"),
		RoutesPath:      filepath.Join(root, "route-bindings.json"),
		ListenerPath:    filepath.Join(root, "route-listener.json"),
		DiscoveryPath:   filepath.Join(root, "provider-discovery.json"),
		GatewayAddr:     "127.0.0.1:0",
		GatewayStateDir: filepath.Join(root, "gateway"),
		Lookup:          func(string) (string, bool) { return "", false },
		Verifier:        BuiltinEnvelopeVerifier{},
		Credentials:     creds,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	owner.upstreamAPIDetect = func(executorID, baseURL, secret string) (string, bool) {
		return detectUpstreamAPI(NewSafeHTTPClient(upstreamAPIProbeTimeout), executorID, baseURL, secret)
	}
	return owner
}

func addConnection(t *testing.T, owner *Owner, client, name, baseURL, model string) Profile {
	t.Helper()
	cat, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: name, Client: client, PresetID: ProviderPresetCustom, BaseURL: baseURL, ModelID: model, Advanced: true,
	}, "fixture-key-not-real", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, c := range cat.Connections {
		if c.Name == name {
			id = c.ID
		}
	}
	if _, err := owner.DiscoverProviderModelsDetailed(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderConnection(client, id, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	profile, err := owner.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func post(t *testing.T, url, body string, header map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func TestDetectUpstreamAPIPrefersTheAgentsOwn(t *testing.T) {
	cases := []struct {
		executor, path, want string
	}{
		{ExecutorCodex, "/v1/responses", ""},
		{ExecutorCodex, "/v1/messages", ProtocolAnthropicMessages},
		{ExecutorCodex, "/v1/chat/completions", ProtocolOpenAIChat},
		{ExecutorClaude, "/v1/messages", ""},
		{ExecutorClaude, "/v1/chat/completions", ProtocolOpenAIChat},
	}
	for _, c := range cases {
		up := &oneAPIUpstream{path: c.path}
		srv := httptest.NewServer(up)
		got, sure := detectUpstreamAPI(NewSafeHTTPClient(upstreamAPIProbeTimeout), c.executor, srv.URL+"/v1", "k")
		srv.Close()
		if got != c.want || !sure {
			t.Errorf("%s on %s: got %q sure=%v, want %q", c.executor, c.path, got, sure, c.want)
		}
	}
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer busy.Close()
	if _, sure := detectUpstreamAPI(NewSafeHTTPClient(upstreamAPIProbeTimeout), ExecutorCodex, busy.URL+"/v1", "k"); sure {
		t.Fatalf("a 502 tells nothing about which API is served")
	}
}

func TestClaudeSessionOnAChatOnlyProvider(t *testing.T) {
	up := &oneAPIUpstream{
		path:   "/v1/chat/completions",
		models: `{"data":[{"id":"glm-5"},{"id":"glm-5-air"}]}`,
		reply: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"pong\"}}]}\n\n" +
			"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n",
	}
	srv := httptest.NewServer(up)
	defer srv.Close()
	owner := detectingOwner(t)
	profile := addConnection(t, owner, ClientClaude, "Chat Relay", srv.URL+"/v1", "glm-5")
	if profile.UpstreamAPI != ProtocolOpenAIChat {
		t.Fatalf("upstream_api = %q, want openai_chat found when the connection was saved", profile.UpstreamAPI)
	}
	plan, err := owner.PrepareLaunchModel(ExecutorClaude, "", "glm-5", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "chat-fixture"); err != nil {
		t.Fatal(err)
	}
	root := plan.Env[EnvAnthropicBaseURL]

	status, body := post(t, root+"/v1/messages", `{"model":"glm-5","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"ping"}]}`,
		map[string]string{"Anthropic-Beta": "interleaved-thinking-2025-05-14"})
	if status != 200 || !strings.Contains(body, "event: message_start") || !strings.Contains(body, `"text":"pong"`) || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("status=%d body=%s", status, body)
	}
	sent, header := up.last()
	if !strings.Contains(sent, `"model":"glm-5"`) || !strings.Contains(sent, `"role":"user"`) || header.Get("Anthropic-Beta") != "" || header.Get("Authorization") != "Bearer fixture-key-not-real" {
		t.Fatalf("upstream got %s with %v: want a Chat request with the Provider's key and none of Claude Code's own headers", sent, header)
	}

	// Claude Code asks for its own haiku for titles; the Provider serves
	// none, so a model it serves stands in.
	if status, body := post(t, root+"/v1/messages", `{"model":"claude-haiku-4-5-20251001","max_tokens":5,"messages":[{"role":"user","content":"title"}]}`, nil); status != 200 {
		t.Fatalf("haiku call: status=%d body=%s", status, body)
	}
	if sent, _ := up.last(); !strings.Contains(sent, `"model":"glm-5"`) {
		t.Fatalf("haiku call went upstream as %s, want a served model", sent)
	}

	before := len(up.bodies)
	status, body = post(t, root+"/v1/messages/count_tokens", `{"model":"glm-5","messages":[{"role":"user","content":"`+strings.Repeat("word ", 400)+`"}]}`, nil)
	if status != 200 || !strings.Contains(body, `"input_tokens"`) || len(up.bodies) != before {
		t.Fatalf("count_tokens: status=%d body=%s upstream calls %d→%d: want a local estimate", status, body, before, len(up.bodies))
	}
}

func TestCodexGatewayOnAnAnthropicOnlyProvider(t *testing.T) {
	up := &oneAPIUpstream{
		path:   "/v1/messages",
		models: `{"data":[{"id":"claude-sonnet-5"}]}`,
		reply: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	}
	srv := httptest.NewServer(up)
	defer srv.Close()
	owner := detectingOwner(t)
	profile := addConnection(t, owner, ClientCodex, "Anthropic Relay", srv.URL+"/v1", "claude-sonnet-5")
	if profile.UpstreamAPI != ProtocolAnthropicMessages {
		t.Fatalf("upstream_api = %q, want anthropic_messages", profile.UpstreamAPI)
	}
	g := owner.Gateway()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3425/v1/responses",
		strings.NewReader(`{"model":"claude-sonnet-5","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}]}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "response.completed") || !strings.Contains(res.Body.String(), `"delta":"pong"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	sent, header := up.last()
	if header.Get("X-Api-Key") != "fixture-key-not-real" || header.Get("Anthropic-Version") == "" || !strings.Contains(sent, `"max_tokens"`) {
		t.Fatalf("upstream got %s with %v: want a Messages request with the key in x-api-key", sent, header)
	}

	// Codex's WebSocket transport is Responses only: refusing it at once
	// makes Codex fall back to HTTP instead of failing the turn. The
	// takeover points the gateway at the chosen connection like this.
	selected, ok := owner.resolveGatewayUpstream(profile.ID)
	if !ok || selected.Protocol != ProtocolAnthropicMessages {
		t.Fatalf("gateway upstream = %+v, want the connection's detected Messages API", selected)
	}
	g.SetUpstream(selected)
	ws := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3425/v1/responses", nil)
	ws.RemoteAddr = "127.0.0.1:1234"
	ws.Header.Set("Connection", "Upgrade")
	ws.Header.Set("Upgrade", "websocket")
	ws.Header.Set("Sec-WebSocket-Version", "13")
	ws.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	wres := httptest.NewRecorder()
	g.ServeHTTP(wres, ws)
	if wres.Code != http.StatusUpgradeRequired {
		t.Fatalf("websocket status = %d, want 426", wres.Code)
	}
}

func TestClaudeStandIn(t *testing.T) {
	served := []string{"glm-5", "deepseek-v4", "kimi-opus-like"}
	for _, c := range []struct {
		requested, want string
		ok              bool
	}{
		{"claude-haiku-4-5-20251001", "glm-5", true}, // no haiku served: the Session's model
		{"claude-opus-5-5", "kimi-opus-like", true},  // same family preferred
		{"claude-opus-5-5[2m]", "", false},           // a marked id is refused, not replaced
		{"gpt-5.5", "", false},                       // not a Claude id
	} {
		got, ok := claudeStandIn(c.requested, served, "glm-5")
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %q %v, want %q %v", c.requested, got, ok, c.want, c.ok)
		}
	}
}

func TestClaudeBillingLineOnlyGoesToClaude(t *testing.T) {
	line := `{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.332.0b8; cc_entrypoint=cli; cch=1f2e3;"}`
	body := func(model string) string {
		return `{"model":"` + model + `","max_tokens":2,"system":[` + line + `,{"type":"text","text":"You are Claude Code.","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`
	}
	out := string(dropClaudeBillingLine([]byte(body("glm-5"))))
	if strings.Contains(out, "billing") || !strings.Contains(out, `"You are Claude Code."`) || !strings.Contains(out, `"cache_control":{"type":"ephemeral"}`) {
		t.Fatalf("got %s: want only the billing block gone", out)
	}
	for _, model := range []string{"claude-opus-5-5", "us.anthropic.claude-sonnet-5", "anthropic/claude-haiku-5-5"} {
		if got := string(dropClaudeBillingLine([]byte(body(model)))); got != body(model) {
			t.Fatalf("%s lost the line: a Claude model is served by Anthropic or a relay of it", model)
		}
	}
	joined := `{"model":"glm-5","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=1; cch=2;\nYou are Claude Code."}],"messages":[]}`
	if got := string(dropClaudeBillingLine([]byte(joined))); !strings.Contains(got, `"text":"You are Claude Code."`) || strings.Contains(got, "billing") {
		t.Fatalf("got %s: want the rest of a block that starts with the line kept", got)
	}

	// A Session's Claude model on an Anthropic-compatible relay keeps it.
	up := &scripted{replies: []func(http.ResponseWriter){status(200, nil, `{"type":"message","content":[]}`)}}
	_, _, root, _ := routeFixture(t, up)
	if status, _ := post(t, root+"/v1/messages", body("claude-test"), nil); status != 200 || !strings.Contains(up.bodies[0], "billing-header") {
		t.Fatalf("status=%d upstream got %v: want the request as Claude Code sent it", status, up.bodies)
	}
}

// A connection saved by an older Mewla has no upstream_api: the first start
// that can look finds it, without the person saving it again.
func TestStartFindsTheAPIOfConnectionsSavedBefore(t *testing.T) {
	up := &oneAPIUpstream{path: "/v1/chat/completions", models: `{"data":[{"id":"glm-5"}]}`}
	srv := httptest.NewServer(up)
	defer srv.Close()
	root := t.TempDir()
	older, _ := newDurableOwner(t, root) // no detection, as before this build
	saved := addConnection(t, older, ClientClaude, "Saved Earlier", srv.URL+"/v1", "glm-5")
	if saved.UpstreamAPI != "" {
		t.Fatalf("upstream_api = %q before any detection", saved.UpstreamAPI)
	}
	_ = older.Close()

	creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:      filepath.Join(root, "model-profiles.toml"),
		RoutesPath:        filepath.Join(root, "route-bindings.json"),
		ListenerPath:      filepath.Join(root, "route-listener.json"),
		DiscoveryPath:     filepath.Join(root, "provider-discovery.json"),
		Lookup:            func(string) (string, bool) { return "", false },
		Verifier:          BuiltinEnvelopeVerifier{},
		Credentials:       creds,
		DetectUpstreamAPI: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, err := owner.store.Get(saved.ID)
		if err == nil && p.UpstreamAPI == ProtocolOpenAIChat {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("upstream_api = %q (err %v) after start, want openai_chat", p.UpstreamAPI, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
