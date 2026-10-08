package modelprofiles

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestGatewayForwardsCodexToolEndpointsToSelectedConnection: Codex's built-in
// image_gen and web.run call standalone endpoints whose body model is a tool
// model, so the gateway asks for the selected connection, forwards the exact
// bytes once, and never leaks the actor marker upstream.
func TestGatewayForwardsCodexToolEndpointsToSelectedConnection(t *testing.T) {
	var calls atomic.Int32
	var gotPath, gotMarker string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotPath = r.URL.Path
		gotMarker = r.Header.Get(CodexActorMarkerHeader)
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1n"}]}`)
	}))
	defer upstream.Close()
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(protocol, model string) (GatewayUpstream, error) {
		if protocol != GatewayProtocolCodexTools || model != "" {
			t.Fatalf("resolver got protocol=%q model=%q", protocol, model)
		}
		return GatewayUpstream{ProfileID: "p", BaseURL: upstream.URL + "/v1", Protocol: ProtocolOpenAIResponses}, nil
	}))
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	for _, path := range []string{"/v1/images/generations", "/v1/images/edits", "/v1/alpha/search"} {
		payload := []byte(`{"model":"gpt-image-2","prompt":"cat"}`)
		req := httptest.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+path, bytes.NewReader(payload))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set(CodexActorMarkerHeader, GatewayProviderName)
		res := httptest.NewRecorder()
		g.ServeHTTP(res, req)
		if res.Code != http.StatusOK || gotPath != path || !bytes.Equal(gotBody, payload) {
			t.Fatalf("%s: status=%d path=%q body=%q", path, res.Code, gotPath, gotBody)
		}
		if gotMarker != "" {
			t.Fatalf("%s: actor marker leaked upstream: %q", path, gotMarker)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("upstream calls = %d, want 3", calls.Load())
	}
}

// TestGatewayNeverRetriesCodexToolEndpoints: an image call is billed even when
// the upstream answers 5xx, so it must not be replayed.
func TestGatewayNeverRetriesCodexToolEndpoints(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer upstream.Close()
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(string, string) (GatewayUpstream, error) {
		return GatewayUpstream{ProfileID: "p", BaseURL: upstream.URL, Protocol: ProtocolOpenAIResponses}, nil
	}))
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	req := httptest.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"cat"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != http.StatusBadGateway || calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d", res.Code, calls.Load())
	}
}

// TestGatewayCodexToolsResolveSelectedCodexConnection: the tool model is not in
// any catalog, yet the request reaches the selected Codex connection with its
// credential; without a selection the gateway fails honestly.
func TestGatewayCodexToolsResolveSelectedCodexConnection(t *testing.T) {
	owner := startBuiltinVerifierOwner(t)
	creds := NewMemoryCredentialStore()
	owner.creds = creds
	owner.router.creds = creds
	if _, err := owner.resolveGatewayRequest(GatewayProtocolCodexTools, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no selection error = %v", err)
	}
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-other", Name: "other", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://other.example/v1", ModelID: "gpt-6-sol", Advanced: true,
	}, "secret", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-selected", Name: "selected", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://selected.example/v1", ModelID: "gpt-6-sol", Advanced: true,
	}, "secret", proj.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderConnection("codex", "conn-selected", proj.Revision); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery.put("conn-selected", []string{"gpt-6-sol"}, nil)
	owner.mu.Unlock()
	up, err := owner.resolveGatewayRequest(GatewayProtocolCodexTools, "")
	if err != nil {
		t.Fatal(err)
	}
	if up.ProfileID != "conn-selected" || up.BaseURL != "https://selected.example/v1" {
		t.Fatalf("tool upstream = %+v", up)
	}
}

// TestTakeoverProjectsCodexActorMarker: the managed provider block carries the
// marker header Codex requires before it offers image_gen, and a daemon restart
// upgrades an older block in place.
func TestTakeoverProjectsCodexActorMarker(t *testing.T) {
	takeover, _, configPath := takeoverFixture(t)
	if _, err := takeover.Enable(DefaultGatewayListenAddr); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ModelProviders map[string]struct {
			RequiresOpenAIAuth bool              `toml:"requires_openai_auth"`
			HTTPHeaders        map[string]string `toml:"http_headers"`
		} `toml:"model_providers"`
	}
	if _, err := toml.DecodeFile(configPath, &doc); err != nil {
		t.Fatal(err)
	}
	provider := doc.ModelProviders[GatewayProviderName]
	if provider.RequiresOpenAIAuth || provider.HTTPHeaders[CodexActorMarkerHeader] != GatewayProviderName {
		t.Fatalf("projected provider = %+v", provider)
	}

	// A block written before the marker existed is replaced on Repair.
	marker := "http_headers = { \"" + CodexActorMarkerHeader + "\" = \"" + GatewayProviderName + "\" }\n"
	legacy := strings.Replace(string(readFileBytes(t, configPath)), marker, "", 1)
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := takeover.Status(); status.State != TakeoverStateDrifted {
		t.Fatalf("legacy block status = %+v", status)
	}
	if status, err := takeover.Repair(DefaultGatewayListenAddr); err != nil || status.State != TakeoverStateActive {
		t.Fatalf("repair status=%+v err=%v", status, err)
	}
	if got := readFileBytes(t, configPath); !bytes.Contains(got, []byte(marker)) || bytes.Count(got, []byte(takeoverMarkerOpen)) != 1 {
		t.Fatalf("repaired config:\n%s", got)
	}
}
