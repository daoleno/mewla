package modelprofiles

import (
	"context"
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
	return c.adopt()
}

func (c *modelsDevCatalog) adopt() error {
	catalog := c.feed().Catalog()
	c.mu.RLock()
	current := c.UpdatedAt
	c.mu.RUnlock()
	if catalog == nil || catalog.At.Equal(current) {
		return nil
	}
	parsed, err := presentationFromCatalog(catalog)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.UpdatedAt, c.Models = catalog.At, parsed
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
	feed := c.feed()
	if _, _, err := feed.Refresh(ctx, func(catalog *modelfacts.Catalog) error {
		_, err := presentationFromCatalog(catalog)
		return err
	}); err != nil {
		return err
	}
	return c.adopt()
}

// RefreshModelsDevMetadata performs an explicit bounded refresh. It only
// updates presentation metadata; Provider discovery remains authoritative.
func (o *Owner) RefreshModelsDevMetadata(ctx context.Context) error {
	if o == nil || o.modelsDev == nil {
		return fmt.Errorf("models.dev metadata unavailable")
	}
	return o.modelsDev.refresh(ctx)
}

// presentationFromCatalog is the picker's view of the shared catalog: display
// name, context window, modalities, temperature, release date and list prices.
// It errors when the catalog holds no provider at all.
func presentationFromCatalog(catalog *modelfacts.Catalog) (map[string]map[string]modelPresentationMetadata, error) {
	if catalog == nil || len(catalog.Providers) == 0 {
		return nil, fmt.Errorf("models.dev metadata has no providers")
	}
	out := make(map[string]map[string]modelPresentationMetadata, len(catalog.Providers))
	for provider, models := range catalog.Providers {
		items := make(map[string]modelPresentationMetadata, len(models))
		for id, model := range models {
			items[id] = normalizeModelPresentationMetadata(modelPresentationMetadata{
				DisplayName:           model.Name,
				ReleaseDate:           model.ReleaseDate,
				ContextWindow:         model.Context,
				Modalities:            model.Modalities,
				TemperatureSupported:  model.Temperature,
				InputPricePerMillion:  model.Cost.Input,
				OutputPricePerMillion: model.Cost.Output,
			})
		}
		out[provider] = items
	}
	return out, nil
}
