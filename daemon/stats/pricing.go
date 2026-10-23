package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/modelfacts"
	"github.com/daoleno/mewla/daemon/statedir"
)

const (
	pricingSyncEvery = modelfacts.TTL
)

var (
	pricingSyncURL    = modelfacts.URL
	pricingHTTPClient = http.DefaultClient
	pricingProviders  = map[string]bool{"anthropic": true, "openai": true, "xai": true}
)

// modelsDevCost is a model's tariff as the shared catalog states it.
type modelsDevCost = modelfacts.Cost

type pricingRegistry struct {
	mu        sync.RWMutex
	models    map[string]map[string]modelPricing
	updatedAt time.Time
	source    string
}

var prices = &pricingRegistry{
	models: staticProviderPricing(),
	source: "built-in",
}

func clonePricingMap(src map[string]modelPricing) map[string]modelPricing {
	out := make(map[string]modelPricing, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneProviderPricing(src map[string]map[string]modelPricing) map[string]map[string]modelPricing {
	out := make(map[string]map[string]modelPricing, len(src))
	for provider, models := range src {
		out[provider] = clonePricingMap(models)
	}
	return out
}

func staticProviderPricing() map[string]map[string]modelPricing {
	out := map[string]map[string]modelPricing{"anthropic": {}, "openai": {}, "xai": {}}
	for modelID, price := range staticPricing {
		out[inferOfficialProvider(modelID)][modelID] = price
	}
	return out
}

func pricingFeedPath(home string) string {
	return filepath.Join(statedir.Default(home), modelfacts.CacheFile)
}

// pricingFeed is the models.dev catalog for a home directory.
func pricingFeed(home string) *modelfacts.Feed {
	// The model picker reads the same file from the same state directory
	// (see modelfacts), so the catalog is downloaded once.
	path := ""
	if home != "" {
		path = pricingFeedPath(home)
	}
	return modelfacts.Open(modelfacts.Config{URL: pricingSyncURL, Path: path, Client: pricingHTTPClient})
}

func currentProviderPricing(provider, modelID string) (modelPricing, bool) {
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	p, ok := prices.models[normalizePricingProvider(provider)][pricingModelID(modelID)]
	return p, ok
}

// currentPricing remains a test/display convenience for models whose official
// provider is unambiguous. Cost calculation always uses currentProviderPricing.
func currentPricing(modelID string) (modelPricing, bool) {
	return currentProviderPricing(inferOfficialProvider(modelID), modelID)
}

func normalizePricingProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek", "deepseek-api":
		return "deepseek"
	case "openai", "openai-api", "codex":
		return "openai"
	case "anthropic", "anthropic-api", "claude":
		return "anthropic"
	case "xai", "x-ai":
		return "xai"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func inferOfficialProvider(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "claude-"):
		return "anthropic"
	case strings.HasPrefix(modelID, "grok-"):
		return "xai"
	case strings.HasPrefix(modelID, "deepseek-"):
		return "deepseek"
	default:
		return "openai"
	}
}

func pricingModelID(modelID string) string {
	if modelID == "grok-build" {
		return "grok-build-0.1"
	}
	return modelID
}

// adoptSharedPricing applies a catalog another reader of the feed downloaded
// since the last one applied here, so the model picker's refresh also refreshes
// these prices without a second download.
func adoptSharedPricing(home string) (changed bool) {
	catalog := pricingFeed(home).Catalog()
	prices.mu.RLock()
	newer := catalog != nil && catalog.At.After(prices.updatedAt)
	before := cloneProviderPricing(prices.models)
	prices.mu.RUnlock()
	if !newer {
		return false
	}
	at := catalog.At
	fetched, err := pricingFromCatalog(catalog)
	if err != nil {
		return false
	}
	prices.mu.Lock()
	prices.models = mergeProviderPricing(prices.models, fetched)
	prices.updatedAt = at
	prices.source = "models.dev"
	after := cloneProviderPricing(prices.models)
	prices.mu.Unlock()
	return !samePricing(before, after)
}

// samePricing compares two registries by their rates, not by when they were
// fetched.
func samePricing(a, b map[string]map[string]modelPricing) bool {
	strip := func(in map[string]map[string]modelPricing) {
		for _, models := range in {
			for id, p := range models {
				p.updatedAt = time.Time{}
				models[id] = p
			}
		}
	}
	strip(a)
	strip(b)
	return reflect.DeepEqual(a, b)
}

// loadPricingCache applies the catalog the feed already holds, from its cache
// file, on top of the built-in prices. A catalog that cannot establish prices
// leaves the built-ins.
func loadPricingCache(home string) {
	catalog := pricingFeed(home).Catalog()
	if catalog == nil {
		return
	}
	at := catalog.At
	loaded, err := pricingFromCatalog(catalog)
	if err != nil {
		return
	}
	prices.mu.Lock()
	prices.models = mergeProviderPricing(staticProviderPricing(), loaded)
	prices.updatedAt = at
	prices.source = "models.dev"
	prices.mu.Unlock()
}

func mergeProviderPricing(base, override map[string]map[string]modelPricing) map[string]map[string]modelPricing {
	out := cloneProviderPricing(base)
	for provider, models := range override {
		if out[provider] == nil {
			out[provider] = make(map[string]modelPricing)
		}
		for id, price := range models {
			out[provider][id] = price
		}
	}
	return out
}

func pricingIsStale() bool {
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	return prices.updatedAt.IsZero() || time.Since(prices.updatedAt) >= pricingSyncEvery
}

func syncPricing(ctx context.Context, home string) error {
	feed := pricingFeed(home)
	_, at, err := feed.Refresh(ctx, func(catalog *modelfacts.Catalog) error {
		_, err := pricingFromCatalog(catalog)
		return err
	})
	if err != nil {
		return err
	}
	fetched, err := pricingFromCatalog(feed.Catalog())
	if err != nil {
		return err
	}
	prices.mu.Lock()
	prices.models = mergeProviderPricing(prices.models, fetched)
	prices.updatedAt = at
	prices.source = "models.dev"
	prices.mu.Unlock()
	return nil
}

// pricingFromCatalog reads the supported providers' rates out of the shared
// models.dev catalog. A catalog with a malformed rate, an invalid tier or no
// supported rate at all is rejected as a whole.
func pricingFromCatalog(catalog *modelfacts.Catalog) (map[string]map[string]modelPricing, error) {
	if catalog == nil {
		return nil, fmt.Errorf("pricing catalog is unavailable")
	}
	at := catalog.At
	updated := map[string]map[string]modelPricing{}
	count := 0
	for provider, models := range catalog.Providers {
		if !pricingProviders[provider] {
			continue
		}
		for modelID, modelData := range models {
			if modelID == "" {
				return nil, fmt.Errorf("pricing catalog contains an empty model ID")
			}
			if modelData.CostErr != nil {
				return nil, fmt.Errorf("model %q: %w", modelID, modelData.CostErr)
			}
			cost := modelData.Cost
			if !cost.HasRates() {
				continue
			}
			localID := modelID
			if updated[provider] == nil {
				updated[provider] = make(map[string]modelPricing)
			}
			current := modelPricing{displayName: modelDisplayName(localID, modelData.Name), source: "models.dev", provider: provider, updatedAt: at,
				tiered: meaningfulTier(cost.Tiers) || meaningfulTier(cost.ContextTier)}
			var tierErr error
			current.contextTiers, tierErr = parseContextPriceTiers(cost)
			if tierErr != nil {
				return nil, fmt.Errorf("model %q: %w", localID, tierErr)
			}
			for _, rate := range []*float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.CacheWrite5m} {
				if rate != nil && (math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0) {
					return nil, fmt.Errorf("invalid pricing rate for model %q", localID)
				}
			}
			if cost.Input != nil {
				current.input = *cost.Input
				current.present |= rateInput
			}
			if cost.Output != nil {
				current.output = *cost.Output
				current.present |= rateOutput
			}
			if cost.CacheRead != nil {
				current.cacheRead = *cost.CacheRead
				current.present |= rateCacheRead
			}
			if cost.CacheWrite != nil {
				current.cacheCreate = *cost.CacheWrite
				current.present |= rateCacheCreate
			} else if cost.CacheWrite5m != nil {
				current.cacheCreate = *cost.CacheWrite5m
				current.present |= rateCacheCreate
			}
			count++
			updated[provider][localID] = current
		}
	}
	if count == 0 {
		return nil, fmt.Errorf("pricing catalog has no supported rates")
	}
	return updated, nil
}

func modelDisplayName(modelID, sourceName string) string {
	if sourceName != "" {
		return sourceName
	}
	return modelID
}

func meaningfulTier(raw json.RawMessage) bool {
	var value any
	if len(raw) == 0 {
		return false
	}
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	switch v := value.(type) {
	case nil:
		return false
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

const (
	rateInput uint8 = 1 << iota
	rateOutput
	rateCacheRead
	rateCacheCreate
)

func (p modelPricing) priceSource() string {
	if p.source != "" {
		return p.source
	}
	return "built-in"
}

func (p modelPricing) ratePresence() uint8 {
	if p.source != "" {
		return p.present
	}
	var mask uint8
	for i, rate := range []float64{p.input, p.output, p.cacheRead, p.cacheCreate} {
		if rate > 0 {
			mask |= 1 << i
		}
	}
	return mask
}
