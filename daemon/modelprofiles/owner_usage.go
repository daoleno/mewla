package modelprofiles

import "strings"

const providerUsageFile = "provider-usage.json"

// ProviderUsage returns what a connection's model requests used and the last
// ones that failed.
func (o *Owner) ProviderUsage(connectionID string) (ConnectionUsage, error) {
	if o == nil || o.store == nil {
		return ConnectionUsage{}, ErrNotFound
	}
	if _, err := o.store.Get(normalizeID(connectionID)); err != nil {
		return ConnectionUsage{}, err
	}
	return o.usage.of(connectionID), nil
}

// usagePrices finds a model's prices in models.dev under the connection's
// provider, and a Claude model's under Anthropic (a relay that serves Claude
// lists it under its own name, if at all).
func (o *Owner) usagePrices(connectionID, model string) (usagePrices, bool) {
	if o == nil || o.store == nil || o.modelsDev == nil {
		return usagePrices{}, false
	}
	profile, err := o.store.Get(normalizeID(connectionID))
	if err != nil {
		return usagePrices{}, false
	}
	keys := []string{catalogKeyOf(profile)}
	bare := model
	if i := strings.LastIndex(model, "/"); i >= 0 {
		bare = model[i+1:]
	}
	if strings.HasPrefix(bare, "claude-") {
		keys = append(keys, "anthropic")
	}
	for _, key := range keys {
		for _, id := range []string{model, bare} {
			if meta, _, ok := o.modelsDev.lookup(key, id); ok && meta.InputPricePerMillion != nil && meta.OutputPricePerMillion != nil {
				return usagePrices{
					input: meta.InputPricePerMillion, output: meta.OutputPricePerMillion,
					cacheRead: meta.CacheReadPricePerMillion, cacheWrite: meta.CacheWritePricePerMillion,
				}, true
			}
		}
	}
	return usagePrices{}, false
}
