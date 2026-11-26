package modelprofiles

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The owner's 11-26 Mac: the machine gateway's list of his Codex connection's
// models was fetched on 10-15 and never again. Codex asked for gpt-6.2-astra,
// which the Provider served, and the gateway answered 404 to every turn.
func TestGatewayLooksAgainForAModelItsCachedListDoesNotKnow(t *testing.T) {
	var mu sync.Mutex
	listed := []string{"gpt-6-astra"}
	var listHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			listHits.Add(1)
			mu.Lock()
			ids := append([]string(nil), listed...)
			mu.Unlock()
			data := make([]map[string]string, 0, len(ids))
			for _, id := range ids {
				data = append(data, map[string]string{"id": id})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_fixture","object":"response","status":"completed","output":[]}`))
	}))
	t.Cleanup(upstream.Close)

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
		CodexConfigPath: filepath.Join(root, "codex", "config.toml"),
		Lookup:          func(string) (string, bool) { return "", false },
		Verifier:        BuiltinEnvelopeVerifier{},
		Credentials:     creds,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	created, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "xcode", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: upstream.URL + "/v1", ModelID: "gpt-6-astra", Advanced: true,
	}, staleFixtureKey, owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := created.Connections[0].ID
	if _, err := owner.DiscoverProviderModelsDetailed(connID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderConnection(ClientCodex, connID, created.Revision); err != nil {
		t.Fatal(err)
	}
	// The Provider adds a model; the cached list is old.
	mu.Lock()
	listed = []string{"gpt-6-astra", "gpt-6.2-astra"}
	mu.Unlock()
	owner.discovery.mu.Lock()
	e := owner.discovery.entries[connID]
	e.FetchedAt = e.FetchedAt.Add(-42 * 24 * time.Hour)
	owner.discovery.entries[connID] = e
	owner.discovery.mu.Unlock()

	if _, err := owner.resolveGatewayRequest(GatewayProtocolResponses, "gpt-6.2-astra"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cache-only answer for the new model = %v, want not found (the stale list)", err)
	}
	before := listHits.Load()
	post := func(model string) int {
		t.Helper()
		res, err := http.Post("http://"+owner.Gateway().ActualAddr()+"/v1/responses", "application/json",
			strings.NewReader(`{"model":"`+model+`","input":"hi"}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	if code := post("gpt-6.2-astra"); code != http.StatusOK {
		t.Fatalf("the gateway answered %d for a model its Provider serves", code)
	}
	if got := listHits.Load() - before; got != 1 {
		t.Fatalf("model list fetched %d times, want 1", got)
	}
	// A name no Provider serves is still refused, and asking again at once
	// does not reach the Provider again.
	for i := 0; i < 3; i++ {
		if code := post("no-such-model"); code != http.StatusNotFound {
			t.Fatalf("unknown model answered %d, want 404", code)
		}
	}
	if got := listHits.Load() - before; got != 1 {
		t.Fatalf("junk model names reached the Provider: %d list fetches", got)
	}
}
