package modelprofiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestVendorPresetsCompileForBothAgents(t *testing.T) {
	for _, tc := range []struct {
		preset, client, base, protocol, auth string
	}{
		{ProviderPresetKimi, ClientClaude, "https://api.moonshot.ai/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetKimi, ClientCodex, "https://api.moonshot.ai/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetKimiCN, ClientClaude, "https://api.moonshot.cn/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetGLM, ClientClaude, "https://api.z.ai/api/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetGLMCN, ClientCodex, "https://open.bigmodel.cn/api/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetMiniMax, ClientClaude, "https://api.minimax.io/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetMiniMaxCN, ClientCodex, "https://api.minimaxi.com/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		// OpenRouter: Codex as before; Claude Code at its Anthropic API.
		{ProviderPresetOpenRouter, ClientCodex, "https://openrouter.ai/api/v1", ProtocolOpenAIResponses, AuthModeBearerEnv},
		{ProviderPresetOpenRouter, ClientClaude, "https://openrouter.ai/api", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
		{ProviderPresetDeepSeek, ClientClaude, "https://api.deepseek.com/anthropic", ProtocolAnthropicMessages, AuthModeXAPIKeyEnv},
	} {
		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: tc.preset + " " + tc.client, Client: tc.client, PresetID: tc.preset})
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.preset, tc.client, err)
		}
		target, err := CompileConnectionTarget(conn, tc.client, "", "")
		if err != nil {
			t.Fatalf("%s/%s target: %v", tc.preset, tc.client, err)
		}
		if target.BaseURL != tc.base || target.Protocol != tc.protocol || target.AuthMode != tc.auth {
			t.Errorf("%s/%s: base=%q protocol=%q auth=%q", tc.preset, tc.client, target.BaseURL, target.Protocol, target.AuthMode)
		}
		if inferPresetID(conn) != tc.preset {
			t.Errorf("%s/%s reads back as preset %q", tc.preset, tc.client, inferPresetID(conn))
		}
	}
}

func TestPresetCatalogKeysAndModelLists(t *testing.T) {
	kimiCN, _ := CompileProviderConnection(ProviderConnectionInput{Name: "k", Client: ClientClaude, PresetID: ProviderPresetKimiCN})
	if got := catalogKeyOf(kimiCN); got != "moonshotai" {
		t.Fatalf("Kimi (China) catalog key %q", got)
	}
	if got := presetModelsURL(kimiCN); got != "https://api.moonshot.cn/v1/models" {
		t.Fatalf("models URL %q", got)
	}
	// Its key goes to the OpenAI-side list as a bearer as well.
	req, _ := http.NewRequest(http.MethodGet, presetModelsURL(kimiCN), nil)
	lookup := func(string) (string, bool) { return "fixture-key-not-real", true }
	probe, err := CompileConnectionTarget(kimiCN, ClientClaude, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if presetModelsURL(probe) != presetModelsURL(kimiCN) {
		t.Fatalf("the compiled target lists at %q", presetModelsURL(probe))
	}
	if err := applyDiscoveryAuth(req, probe, nil, lookup); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer fixture-key-not-real" || req.Header.Get("X-Api-Key") != "fixture-key-not-real" {
		t.Fatalf("headers %v", req.Header)
	}
	// A connection moved to another address lists its models there.
	moved := kimiCN
	moved.BaseURL = "https://relay.example.com/anthropic"
	if presetModelsURL(moved) != "" {
		t.Fatal("a moved connection still lists the vendor's models")
	}
	if got := catalogKeyOf(Profile{ProviderID: "custom"}); got != "custom" {
		t.Fatalf("custom catalog key %q", got)
	}
}

func TestLocalPresetsTakeNoKeyAndFindTheirAPI(t *testing.T) {
	owner, _ := newDurableOwner(t, t.TempDir())
	defer owner.Close()
	var asked []string
	owner.upstreamAPIDetect = func(executorID, baseURL, secret string) (string, bool) {
		asked = append(asked, baseURL+"|"+secret)
		return ProtocolOpenAIChat, true
	}
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{Name: "Ollama", Client: ClientClaude, PresetID: ProviderPresetOllama}, "", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "http://localhost:11434|" {
		t.Fatalf("detection asked %v", asked)
	}
	var conn ProviderConnection
	for _, c := range proj.Connections {
		if c.Name == "Ollama" {
			conn = c
		}
	}
	if !conn.CredentialReady || conn.PresetID != ProviderPresetOllama || conn.BaseURL != "http://localhost:11434" || conn.UpstreamAPI != ProtocolOpenAIChat {
		t.Fatalf("connection %+v", conn)
	}
	stored, err := owner.store.Get(conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := CompileConnectionTarget(stored, ClientClaude, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.AuthMode != AuthModeNone || target.Protocol != ProtocolOpenAIChat {
		t.Fatalf("target auth=%q protocol=%q", target.AuthMode, target.Protocol)
	}
	// Another port is the person's to give.
	moved, err := owner.UpsertProviderConnection(ProviderConnectionInput{ID: conn.ID, Name: "Ollama", Client: ClientClaude, PresetID: ProviderPresetOllama, BaseURL: "http://localhost:11500"}, "", owner.Catalog().Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range moved.Connections {
		if c.ID == conn.ID && (c.BaseURL != "http://localhost:11500" || c.PresetID != ProviderPresetOllama) {
			t.Fatalf("moved %+v", c)
		}
	}
	// "Local server" has no address of its own.
	if _, err := CompileProviderConnection(ProviderConnectionInput{Name: "vLLM", Client: ClientCodex, PresetID: ProviderPresetLocal}); err == nil {
		t.Fatal("a local server was saved without an address")
	}
}

func TestPresetListShowsLocalAddressesOnly(t *testing.T) {
	byID := map[string]ProviderPreset{}
	for _, p := range ListProviderPresets() {
		byID[p.ID] = p
	}
	if p := byID[ProviderPresetOllama]; !p.Local || p.BaseURL != "http://localhost:11434" {
		t.Fatalf("ollama %+v", p)
	}
	if p := byID[ProviderPresetLocal]; !p.Local || p.BaseURL != "" {
		t.Fatalf("local %+v", p)
	}
	for _, id := range []string{ProviderPresetKimi, ProviderPresetGLM, ProviderPresetMiniMax, ProviderPresetOpenRouter} {
		if p := byID[id]; p.Local || p.BaseURL != "" || len(p.Clients) != 2 {
			t.Fatalf("%s %+v", id, p)
		}
	}
}

func TestLocalServersAreFoundByTheirModelList(t *testing.T) {
	serve := func(status int, body string) (*httptest.Server, int) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		u, _ := url.Parse(srv.URL)
		port, _ := strconv.Atoi(u.Port())
		return srv, port
	}
	ollama, p1 := serve(200, `{"object":"list","data":[{"id":"llama3:latest"},{"id":"gemma2:latest"}]}`)
	defer ollama.Close()
	page, p2 := serve(200, `<html>a web app</html>`)
	defer page.Close()
	gone, p3 := serve(404, `{}`)
	gone.Close()
	empty, p4 := serve(200, `{"data":[]}`)
	defer empty.Close()
	found := probeLocalProviders(context.Background(), NewSafeHTTPClient(localProbeTimeout), "127.0.0.1", []localTarget{
		{ProviderPresetOllama, p1}, {ProviderPresetLocal, p2}, {ProviderPresetLMStudio, p3}, {ProviderPresetLocal, p4},
	})
	if len(found) != 2 || found[0].PresetID != ProviderPresetOllama || found[0].Models != 2 || found[0].Label != "Ollama" || found[1].Models != 0 {
		t.Fatalf("found %+v", found)
	}
	if !sameLocalAddress("http://127.0.0.1:11434/v1", "http://localhost:11434") || sameLocalAddress("http://localhost:1234", "http://localhost:11434") {
		t.Fatal("address match")
	}
}
