package modelprofiles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/modelfacts"
)

// modelsDevCacheFile is the shared models.dev catalog file, the one the usage
// stats read as well (see modelfacts).
const modelsDevCacheFile = modelfacts.CacheFile

// ModelsDevURL is the public models.dev metadata feed the daemon refreshes.
const ModelsDevURL = modelfacts.URL

// modelsDevCatalog is the picker's view of the models.dev catalog: display
// names, context windows and list prices. The download and its cache belong to
// modelfacts; this keeps what it parsed from the copy it last saw.
type modelsDevCatalog struct {
	mu        sync.RWMutex
	UpdatedAt time.Time                                       `json:"updated_at"`
	Models    map[string]map[string]modelPresentationMetadata `json:"models"`
	path      string
	url       string // empty disables refresh
	client    *http.Client
}

func newModelsDevCatalog(path, url string) *modelsDevCatalog {
	return &modelsDevCatalog{path: path, url: strings.TrimSpace(url), Models: map[string]map[string]modelPresentationMetadata{}}
}

func (c *modelsDevCatalog) feed() *modelfacts.Feed {
	return modelfacts.Open(modelfacts.Config{URL: c.url, Path: c.path, Client: c.client})
}

// load parses the catalog the feed already holds, from its cache file.
func (c *modelsDevCatalog) load() error {
	if c == nil || c.path == "" {
		return nil
	}
	return c.sync()
}

// sync parses the feed's catalog when it is newer than the one parsed. Another
// reader's refresh shows up here without a download of its own.
func (c *modelsDevCatalog) sync() error {
	if c == nil || c.path == "" {
		return nil
	}
	raw, at := c.feed().Raw()
	c.mu.RLock()
	current := c.UpdatedAt
	c.mu.RUnlock()
	if raw == nil || at.Equal(current) {
		return nil
	}
	parsed, err := parseModelsDevMetadata(raw)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.UpdatedAt, c.Models = at, parsed
	c.mu.Unlock()
	return nil
}

func (c *modelsDevCatalog) stale() bool {
	if c == nil {
		return true
	}
	if c.path == "" {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.UpdatedAt.IsZero() || time.Since(c.UpdatedAt) >= modelfacts.TTL
	}
	return c.feed().Stale(modelfacts.TTL)
}

func (c *modelsDevCatalog) lookup(provider, id string) (modelPresentationMetadata, time.Time, bool) {
	_ = c.sync()
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := c.Models[strings.ToLower(strings.TrimSpace(provider))]
	item, ok := items[strings.TrimSpace(id)]
	return item, c.UpdatedAt, ok
}

// modelIDs returns validated ids present in the local models.dev cache. This
// cache is metadata-only input and is used here only as a local fallback.
func (c *modelsDevCatalog) modelIDs(provider string) []string {
	if c == nil {
		return nil
	}
	_ = c.sync()
	c.mu.RLock()
	items := c.Models[strings.ToLower(strings.TrimSpace(provider))]
	ids := make([]string, 0, len(items))
	for id := range items {
		id = normalizeSpace(id)
		if ValidateModelID(id) == nil {
			ids = append(ids, id)
		}
	}
	c.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

func (c *modelsDevCatalog) refresh(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if c.url == "" {
		return fmt.Errorf("models.dev metadata refresh is disabled")
	}
	raw, at, err := c.feed().Refresh(ctx, func(raw []byte) error {
		_, err := parseModelsDevMetadata(raw)
		return err
	})
	if err != nil {
		return err
	}
	parsed, err := parseModelsDevMetadata(raw)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.UpdatedAt, c.Models = at, parsed
	c.mu.Unlock()
	return nil
}

// RefreshModelsDevMetadata performs an explicit bounded refresh. It only
// updates presentation metadata; Provider discovery remains authoritative.
func (o *Owner) RefreshModelsDevMetadata(ctx context.Context) error {
	if o == nil || o.modelsDev == nil {
		return fmt.Errorf("models.dev metadata unavailable")
	}
	return o.modelsDev.refresh(ctx)
}

func parseModelsDevMetadata(raw []byte) (map[string]map[string]modelPresentationMetadata, error) {
	var providers map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, err
	}
	out := map[string]map[string]modelPresentationMetadata{}
	for provider, value := range providers {
		for id, rawModel := range value.Models {
			var model map[string]json.RawMessage
			if json.Unmarshal(rawModel, &model) != nil {
				continue
			}
			meta := modelPresentationMetadata{DisplayName: jsonString(model["name"]), ReleaseDate: jsonString(model["release_date"])}
			meta.ContextWindow = jsonInt(model["context"])
			if meta.ContextWindow == 0 {
				meta.ContextWindow = jsonInt(model["context_window"])
			}
			if meta.ContextWindow == 0 {
				var limit map[string]json.RawMessage
				if json.Unmarshal(model["limit"], &limit) == nil {
					meta.ContextWindow = jsonInt(limit["context"])
				}
			}
			meta.InputPricePerMillion = jsonFloat(model["input"])
			meta.OutputPricePerMillion = jsonFloat(model["output"])
			if cost, ok := model["cost"]; ok {
				var c map[string]json.RawMessage
				_ = json.Unmarshal(cost, &c)
				if meta.InputPricePerMillion == nil {
					meta.InputPricePerMillion = jsonFloat(c["input"])
				}
				if meta.OutputPricePerMillion == nil {
					meta.OutputPricePerMillion = jsonFloat(c["output"])
				}
			}
			if v, ok := model["temperature"]; ok {
				var b bool
				if json.Unmarshal(v, &b) == nil {
					meta.TemperatureSupported = &b
				}
			}
			meta.Modalities = jsonStrings(model["modalities"])
			for _, effort := range jsonStrings(model["reasoning_efforts"]) {
				meta.SupportedReasoningLevels = append(meta.SupportedReasoningLevels, CodexReasoningEffortPreset{Effort: effort})
			}
			meta = normalizeModelPresentationMetadata(meta)
			if out[strings.ToLower(provider)] == nil {
				out[strings.ToLower(provider)] = map[string]modelPresentationMetadata{}
			}
			out[strings.ToLower(provider)][id] = meta
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("models.dev metadata has no providers")
	}
	return out, nil
}

func jsonString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return strings.TrimSpace(s)
}
func jsonFloat(raw json.RawMessage) *float64 {
	var n float64
	if len(raw) == 0 || json.Unmarshal(raw, &n) != nil || n < 0 {
		return nil
	}
	return &n
}
func jsonInt(raw json.RawMessage) int64 {
	var n int64
	_ = json.Unmarshal(raw, &n)
	if n < 0 {
		return 0
	}
	return n
}
func jsonStrings(raw json.RawMessage) []string {
	var a []string
	if json.Unmarshal(raw, &a) == nil {
		return a
	}
	var o map[string][]string
	if json.Unmarshal(raw, &o) == nil {
		for _, values := range o {
			a = append(a, values...)
		}
	}
	return a
}
