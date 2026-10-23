package modelfacts

import (
	"context"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const sample = `{
  "OpenAI": {"models": {
    "gpt-x": {"name": " GPT X ", "release_date": "2026-01-02", "temperature": false,
      "limit": {"context": 400000, "output": 128000},
      "modalities": {"input": ["text", "image"], "output": ["text"]},
      "cost": {"input": 1.25, "output": 10, "cache_read": 0.125, "tiers": [{"tier": {"type": "context", "size": 200000}, "input": 2}]}},
    "no-cost": {"name": "No Cost"},
    "bad-cost": {"name": "Bad", "cost": {"input": "free"}},
    "broken": 7
  }},
  "notaprovider": true
}`

func TestParseReadsTheFactsReadersNeed(t *testing.T) {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	catalog, err := Parse([]byte(sample), at)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := catalog.Model(" openai ", "gpt-x")
	if !ok {
		t.Fatalf("providers = %v", catalog.Providers)
	}
	if model.Name != "GPT X" || model.ReleaseDate != "2026-01-02" || model.Context != 400000 {
		t.Fatalf("model = %+v", model)
	}
	if model.Cost.Input == nil || *model.Cost.Input != 1.25 || *model.Cost.CacheRead != 0.125 || model.Cost.CacheWrite != nil || !model.Cost.HasRates() {
		t.Fatalf("cost = %+v", model.Cost)
	}
	if len(model.Cost.Tiers) == 0 {
		t.Fatal("tiers were dropped")
	}
	if got := model.Modalities; len(got) != 3 || got[0] != "text" || got[1] != "image" || got[2] != "text" {
		t.Fatalf("modalities = %v", got)
	}
	if model.Temperature == nil || *model.Temperature {
		t.Fatalf("temperature = %v", model.Temperature)
	}
	if free, _ := catalog.Model("openai", "no-cost"); free.Cost.HasRates() || free.CostErr != nil {
		t.Fatalf("no-cost = %+v", free)
	}
	if bad, _ := catalog.Model("openai", "bad-cost"); bad.CostErr == nil || bad.Cost.HasRates() {
		t.Fatalf("a cost block that is not numbers must be flagged: %+v", bad)
	}
	if _, ok := catalog.Model("openai", "broken"); ok {
		t.Fatal("an unreadable model was kept")
	}
	if ids := catalog.ModelIDs("OpenAI"); len(ids) != 3 || ids[0] != "bad-cost" {
		t.Fatalf("ids = %v", ids)
	}
	if catalog.Models() != 3 || catalog.At != at {
		t.Fatalf("models=%d at=%v", catalog.Models(), catalog.At)
	}
	var none *Catalog
	if _, ok := none.Model("openai", "gpt-x"); ok || none.ModelIDs("openai") != nil || none.Models() != 0 {
		t.Fatal("a nil catalog has facts")
	}
	if _, err := Parse([]byte(`[1]`), at); err == nil {
		t.Fatal("a document that is not an object parsed")
	}
}

func TestFeedParsesEachDownloadOnce(t *testing.T) {
	var hits atomic.Int64
	server := countingServer(t, sample, &hits, nil)
	path := filepath.Join(t.TempDir(), "models-dev.json")
	feed := Open(Config{URL: server.URL, Path: path})
	if feed.Catalog() != nil {
		t.Fatal("a feed nobody downloaded has a catalog")
	}
	if _, _, err := feed.Refresh(context.Background(), func(c *Catalog) error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, second := feed.Catalog(), feed.Catalog()
	if first == nil || first != second {
		t.Fatalf("each reader got its own parse: %p %p", first, second)
	}
	if _, ok := first.Model("openai", "gpt-x"); !ok || time.Since(first.At) > time.Minute {
		t.Fatalf("catalog = %+v", first)
	}

	// A new process reads the cache file and parses it once for all readers.
	registryMu.Lock()
	delete(registry, server.URL+"\x00"+path)
	registryMu.Unlock()
	reopened := Open(Config{URL: server.URL, Path: path, Client: http.DefaultClient})
	if a, b := reopened.Catalog(), reopened.Catalog(); a == nil || a != b {
		t.Fatalf("cache file catalog = %p %p", a, b)
	}
}
