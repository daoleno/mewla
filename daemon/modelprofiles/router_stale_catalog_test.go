package modelprofiles

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// staleCatalogFixture is a custom Anthropic connection whose upstream model
// list can change after the daemon cached it.
type staleCatalogFixture struct {
	t         *testing.T
	owner     *Owner
	upstream  *httptest.Server
	routeRoot string
	connID    string
	mu        sync.Mutex
	listed    []string
	listDown  bool
	listHits  atomic.Int32
	served    []string
}

const staleFixtureKey = "fixture-key-not-real"

func newStaleCatalogFixture(t *testing.T, listed ...string) *staleCatalogFixture {
	t.Helper()
	f := &staleCatalogFixture{t: t, listed: listed}
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			f.listHits.Add(1)
			f.mu.Lock()
			down, ids := f.listDown, append([]string(nil), f.listed...)
			f.mu.Unlock()
			if down {
				http.Error(w, "upstream unavailable", http.StatusBadGateway)
				return
			}
			data := make([]map[string]string, 0, len(ids))
			for _, id := range ids {
				data = append(data, map[string]string{"id": id})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.mu.Lock()
		f.served = append(f.served, payload.Model)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_fixture","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(f.upstream.Close)
	owner, _ := newDurableOwner(t, t.TempDir())
	t.Cleanup(func() { _ = owner.Close() })
	f.owner = owner
	created, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Studio Gateway", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: f.upstream.URL + "/v1", ModelID: listed[0], Advanced: true,
	}, staleFixtureKey, owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	f.connID = created.Connections[0].ID
	if _, err := owner.DiscoverProviderModelsDetailed(f.connID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderConnection(ClientClaude, f.connID, created.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunchModel(ExecutorClaude, "", listed[0], "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "stale-catalog-fixture"); err != nil {
		t.Fatal(err)
	}
	f.routeRoot = plan.Env[EnvAnthropicBaseURL]
	return f
}

// age makes the cached model list look as old as the real stale one was.
func (f *staleCatalogFixture) age(by time.Duration) {
	f.owner.discovery.mu.Lock()
	defer f.owner.discovery.mu.Unlock()
	e := f.owner.discovery.entries[f.connID]
	e.FetchedAt = e.FetchedAt.Add(-by)
	f.owner.discovery.entries[f.connID] = e
}

func (f *staleCatalogFixture) setUpstream(down bool, ids ...string) {
	f.mu.Lock()
	f.listDown, f.listed = down, ids
	f.mu.Unlock()
}

func (f *staleCatalogFixture) message(model string) (int, string) {
	f.t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.routeRoot+"/v1/messages", bytes.NewBufferString(`{"model":"`+model+`","max_tokens":2,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func (f *staleCatalogFixture) servedModels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.served...)
}

func TestStaleCatalogRediscoversModelAddedUpstream(t *testing.T) {
	f := newStaleCatalogFixture(t, "claude-old")
	f.setUpstream(false, "claude-old", "claude-new")
	f.age(3 * 24 * time.Hour) // cached three days ago, as in the 11-11 incident
	before := f.listHits.Load()

	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = f.message("claude-new")
		}(i)
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d for a model the upstream now lists got %d", i, code)
		}
	}
	if got := f.listHits.Load() - before; got != 1 {
		t.Fatalf("concurrent misses must share one rediscovery, upstream list fetched %d times", got)
	}
	// Later requests are served from the refreshed cache without another fetch.
	if code, _ := f.message("claude-new"); code != http.StatusOK || f.listHits.Load()-before != 1 {
		t.Fatalf("refreshed list must be cached: code=%d fetches=%d", code, f.listHits.Load()-before)
	}
	if got := f.servedModels(); len(got) != 7 || got[0] != "claude-new" {
		t.Fatalf("upstream received %v", got)
	}
}

func TestStaleCatalogRefusesModelUpstreamDoesNotList(t *testing.T) {
	f := newStaleCatalogFixture(t, "claude-old")
	f.age(3 * 24 * time.Hour)
	before := f.listHits.Load()

	code, body := f.message("claude-ghost")
	if code != http.StatusNotFound {
		t.Fatalf("unlisted model status = %d, want 404: %s", code, body)
	}
	var payload struct {
		Error struct{ Type, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("refusal is not JSON: %v: %s", err, body)
	}
	msg := payload.Error.Message
	if payload.Error.Type != "model_not_found" || !strings.Contains(msg, `"claude-ghost"`) || !strings.Contains(msg, `"Studio Gateway"`) || strings.Contains(msg, "request failed") {
		t.Fatalf("refusal must name the model and the connection: %s", body)
	}
	for _, secret := range []string{staleFixtureKey, f.upstream.URL, "127.0.0.1"} {
		if strings.Contains(body, secret) {
			t.Fatalf("refusal leaked %q: %s", secret, body)
		}
	}
	if got := f.listHits.Load() - before; got != 1 {
		t.Fatalf("one miss = one live look, got %d", got)
	}
	// Junk model names must not become upstream traffic.
	for i := 0; i < 5; i++ {
		if code, _ := f.message("claude-ghost"); code != http.StatusNotFound {
			t.Fatalf("repeat refusal status = %d", code)
		}
	}
	if got := f.listHits.Load() - before; got != 1 {
		t.Fatalf("repeated unknown model refetched the list %d times", got)
	}
	if served := f.servedModels(); len(served) != 0 {
		t.Fatalf("a refused model reached the upstream: %v", served)
	}
	if code, _ := f.message("claude-old"); code != http.StatusOK {
		t.Fatalf("a listed model must still work, got %d", code)
	}
}

func TestStaleCatalogKeepsLastKnownGoodWhenUpstreamIsDown(t *testing.T) {
	f := newStaleCatalogFixture(t, "claude-old")
	f.setUpstream(true)
	f.age(3 * 24 * time.Hour)

	code, raw := f.message("claude-new")
	var down struct {
		Error struct{ Message string } `json:"error"`
	}
	_ = json.Unmarshal([]byte(raw), &down)
	body := down.Error.Message
	if code != http.StatusNotFound || !strings.Contains(body, "could not be refreshed") || !strings.Contains(body, `"claude-new"`) || !strings.Contains(body, `"Studio Gateway"`) {
		t.Fatalf("down upstream + unknown model: want a refusal that says the list could not be refreshed, got %d %s", code, body)
	}
	// Last-known-good is untouched: the old model still routes and the list is intact.
	if code, _ := f.message("claude-old"); code != http.StatusOK {
		t.Fatalf("last-known-good model must keep working with the upstream down, got %d", code)
	}
	e, _ := f.owner.discovery.get(f.connID)
	if len(e.LastGood) != 1 || e.LastGood[0] != "claude-old" || len(e.IDs) != 1 || e.IDs[0] != "claude-old" {
		t.Fatalf("failed refresh damaged the last-known-good list: %#v", e)
	}
	// Upstream recovers; once the cooldown passes the next miss finds the model.
	f.setUpstream(false, "claude-old", "claude-new")
	f.age(time.Minute)
	if code, body := f.message("claude-new"); code != http.StatusOK {
		t.Fatalf("recovered upstream: %d %s", code, body)
	}
}
