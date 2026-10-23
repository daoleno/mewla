package modelfacts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Cost is a model's reference tariff in dollars per million tokens. A rate the
// catalog does not state is nil, which is different from a rate of zero.
type Cost struct {
	Input        *float64        `json:"input"`
	Output       *float64        `json:"output"`
	CacheRead    *float64        `json:"cache_read"`
	CacheWrite   *float64        `json:"cache_write"`
	CacheWrite5m *float64        `json:"cache_write_5m"`
	Tiers        json.RawMessage `json:"tiers"`
	ContextTier  json.RawMessage `json:"context_over_200k"`
}

// HasRates reports whether the catalog states any rate at all.
func (c Cost) HasRates() bool {
	return c.Input != nil || c.Output != nil || c.CacheRead != nil || c.CacheWrite != nil || c.CacheWrite5m != nil
}

// Model is what the catalog says about one model.
type Model struct {
	// ID is the model's key under its provider.
	ID          string
	Name        string
	ReleaseDate string
	// Context is the context window in tokens, 0 when not stated.
	Context int64
	Cost    Cost
	// CostErr is set when the cost block is not made of numbers; Cost is then
	// empty. Readers that must trust prices reject the catalog on it.
	CostErr error
	// Modalities lists the input modalities, then the output ones.
	Modalities  []string
	Temperature *bool
}

// Catalog is one parsed copy of the models.dev document: the typed facts every
// reader of the feed works from, keyed by lower-case provider.
type Catalog struct {
	// At is when the document was downloaded.
	At        time.Time
	Providers map[string]map[string]Model
}

// Model returns one provider's model.
func (c *Catalog) Model(provider, id string) (Model, bool) {
	if c == nil {
		return Model{}, false
	}
	model, ok := c.Providers[strings.ToLower(strings.TrimSpace(provider))][strings.TrimSpace(id)]
	return model, ok
}

// ModelIDs lists a provider's model ids, sorted.
func (c *Catalog) ModelIDs(provider string) []string {
	if c == nil {
		return nil
	}
	models := c.Providers[strings.ToLower(strings.TrimSpace(provider))]
	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Models counts every model of every provider.
func (c *Catalog) Models() int {
	if c == nil {
		return 0
	}
	count := 0
	for _, models := range c.Providers {
		count += len(models)
	}
	return count
}

// Parse reads the facts out of a models.dev document. A provider or model it
// cannot read is left out; only a document that is not a JSON object fails.
func Parse(raw []byte, at time.Time) (*Catalog, error) {
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("models.dev catalog: %w", err)
	}
	catalog := &Catalog{At: at, Providers: map[string]map[string]Model{}}
	for name, rawProvider := range providers {
		var provider struct {
			Models map[string]json.RawMessage `json:"models"`
		}
		if json.Unmarshal(rawProvider, &provider) != nil {
			continue
		}
		key := strings.ToLower(name)
		for id, rawModel := range provider.Models {
			var wire struct {
				Name        string          `json:"name"`
				ReleaseDate string          `json:"release_date"`
				Temperature *bool           `json:"temperature"`
				Cost        json.RawMessage `json:"cost"`
				Limit       struct {
					Context int64 `json:"context"`
				} `json:"limit"`
				Modalities struct {
					Input  []string `json:"input"`
					Output []string `json:"output"`
				} `json:"modalities"`
			}
			if json.Unmarshal(rawModel, &wire) != nil {
				continue
			}
			model := Model{ID: id, Name: strings.TrimSpace(wire.Name), ReleaseDate: strings.TrimSpace(wire.ReleaseDate), Temperature: wire.Temperature}
			if wire.Limit.Context > 0 {
				model.Context = wire.Limit.Context
			}
			if len(wire.Cost) > 0 {
				if err := json.Unmarshal(wire.Cost, &model.Cost); err != nil {
					model.Cost, model.CostErr = Cost{}, err
				}
			}
			model.Modalities = append(append([]string(nil), wire.Modalities.Input...), wire.Modalities.Output...)
			if catalog.Providers[key] == nil {
				catalog.Providers[key] = map[string]Model{}
			}
			catalog.Providers[key][id] = model
		}
	}
	return catalog, nil
}

// Catalog returns the parsed catalog the feed holds, parsed once per download,
// or nil when there is none.
func (f *Feed) Catalog() *Catalog {
	raw, at := f.Raw()
	if raw == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.parsed != nil && f.parsed.At.Equal(at) {
		return f.parsed
	}
	catalog, err := Parse(raw, at)
	if err != nil {
		return nil
	}
	f.parsed = catalog
	return catalog
}
