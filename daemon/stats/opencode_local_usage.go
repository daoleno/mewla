package stats

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/opencodeapi"
)

// OpenCode model usage is collected from OpenCode 2's own server API
// (session.list and session.message.list through `opencode api`), the same
// interface Work reads transcripts from. There is no subscription probing, no
// credential read, and no upstream API access: the collector reads only the
// observed usage fields OpenCode records per assistant message (model, token
// counts, cost, timestamp) plus the session directory, and aggregates exactly
// those facts. Messages without usage are ignored, unavailable metrics are
// omitted, and quota or subscription state is never inferred.

// openCodeUsageRow is the usage of one assistant message.
type openCodeUsageRow struct {
	Model    string
	Provider string
	Cost     float64
	CostOK   bool
	Input    float64
	Output   float64
	Reason   float64
	CacheR   float64
	CacheW   float64
	Created  time.Time
	Cwd      string
}

// openCodeSessionUsage caches one session's rows until its update clock moves.
type openCodeSessionUsage struct {
	updated float64
	rows    []openCodeUsageRow
}

type openCodeAssistantUsage struct {
	Model struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	} `json:"model"`
	Cost   *float64 `json:"cost"`
	Tokens struct {
		Input     float64 `json:"input"`
		Output    float64 `json:"output"`
		Reasoning float64 `json:"reasoning"`
		Cache     struct {
			Read  float64 `json:"read"`
			Write float64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Time struct {
		Created float64 `json:"created"`
	} `json:"time"`
}

// openCodeUsageRows returns every assistant usage row, re-reading only
// sessions whose update clock changed since the previous refresh.
func (c *Collector) openCodeUsageRows() ([]openCodeUsageRow, error) {
	ctx := context.Background()
	sessions, err := opencodeapi.ListSessions(ctx, opencodeapi.SessionQuery{})
	if err != nil {
		return nil, err
	}
	c.openCodeUsageMu.Lock()
	defer c.openCodeUsageMu.Unlock()
	next := make(map[string]openCodeSessionUsage, len(sessions))
	var rows []openCodeUsageRow
	for _, session := range sessions {
		cached, ok := c.openCodeUsage[session.ID]
		if !ok || cached.updated != session.Time.Updated {
			messages, err := opencodeapi.ListMessages(ctx, session.ID, "assistant")
			if err != nil {
				return nil, err
			}
			cached = openCodeSessionUsage{updated: session.Time.Updated}
			for _, message := range messages {
				var usage openCodeAssistantUsage
				if json.Unmarshal(message.Raw, &usage) != nil {
					continue
				}
				row := openCodeUsageRow{
					Model:    strings.TrimSpace(usage.Model.ID),
					Provider: usage.Model.ProviderID,
					Input:    usage.Tokens.Input,
					Output:   usage.Tokens.Output,
					Reason:   usage.Tokens.Reasoning,
					CacheR:   usage.Tokens.Cache.Read,
					CacheW:   usage.Tokens.Cache.Write,
					Cwd:      session.Location.Directory,
				}
				if usage.Cost != nil {
					row.Cost, row.CostOK = *usage.Cost, true
				}
				if usage.Time.Created > 0 {
					row.Created = time.UnixMilli(int64(usage.Time.Created))
				}
				cached.rows = append(cached.rows, row)
			}
		}
		next[session.ID] = cached
		rows = append(rows, cached.rows...)
	}
	c.openCodeUsage = next
	return rows, nil
}

// collectOpenCodeStats aggregates OpenCode usage per date: assistant-message
// (request) counts, input/output/reasoning/cache tokens, and cost when those
// exact fields exist. Each usage row counts as one request; rows without any
// token usage contribute nothing; a row without a cost keeps the model's cost
// unknown unless that exact provider/model has a tariff. Rows are aggregated
// by the recorded (provider, model) identity; equal model IDs from different
// providers remain separate.
func (c *Collector) collectOpenCodeStats() map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)

	rows, err := c.openCodeUsageRows()
	if err != nil {
		if !errors.Is(err, opencodeapi.ErrUnavailable) {
			c.warnf("opencode-api", "[stats] OpenCode stats read failed: %v", err)
		}
		return byDate
	}

	for _, row := range rows {
		modelID := row.Model
		if modelID == "" || row.Created.IsZero() {
			continue
		}
		provider := normalizePricingProvider(row.Provider)
		seconds := row.Created.Unix()
		local := row.Created.In(time.Local)
		date := local.Format("2006-01-02")
		hour := local.Hour() / 6
		if hour > 3 {
			hour = 3
		}

		input, output, reasoning := row.Input, row.Output, row.Reason
		cacheRead, cacheWrite := row.CacheR, row.CacheW
		if input+output+reasoning+cacheRead+cacheWrite <= 0 {
			// No observed usage: the row is not a usage row.
			continue
		}
		cost, costOK := row.Cost, row.CostOK

		agg := ensureDateAgg(byDate, date)

		// Per-model aggregation keeps provider-reported and tariff-estimated
		// records as separate additive components.
		key := modelAggKey(provider, modelID)
		model := agg.models[key]
		model.totalTokens += int64(input + output + reasoning + cacheRead + cacheWrite)
		model.inputTokens += int64(input)
		model.outputTokens += int64(output)
		model.reasoning += int64(reasoning)
		model.cacheRead += int64(cacheRead)
		model.cacheCreate += int64(cacheWrite)
		model.sessions++
		if costOK {
			if model.recorded == nil {
				model.recorded = &modelRecordedCost{}
			}
			model.recorded.cost += cost
			model.recorded.input += int64(input)
			model.recorded.output += int64(output)
			model.recorded.reasoning += int64(reasoning)
			model.recorded.cacheRead += int64(cacheRead)
			model.recorded.cacheCreate += int64(cacheWrite)
			model.provider, model.modelID = provider, modelID
		} else {
			// OpenCode reports reasoning separately from output, so both use the
			// provider's output tariff. Input excludes cache.read/write buckets.
			addEstimatedCost(&model, provider, modelID, time.Unix(seconds, 0), int64(input), int64(output+reasoning), int64(cacheRead), int64(cacheWrite))
		}
		agg.models[key] = model

		// Per-project aggregation from the session directory.
		if projectName := openCodeProjectName(row.Cwd); projectName != "" {
			project := ensureProjectAgg(agg, projectName)
			costAvailable := costOK
			project.totalTokens += int64(input + output + reasoning + cacheRead + cacheWrite)
			project.inputTokens += int64(input)
			project.outputTokens += int64(output)
			project.reasoning += int64(reasoning)
			project.cacheRead += int64(cacheRead)
			project.cacheCreate += int64(cacheWrite)
			if costOK {
				project.cost += cost
				project.reportedCost += cost
				project.reported = true
			} else if estimate, ok := computeProviderKnownCost(provider, modelID, time.Unix(seconds, 0), int64(input), int64(output+reasoning), int64(cacheRead), int64(cacheWrite)); ok {
				project.cost += estimate
				project.estimatedCost += estimate
				project.estimated = true
				costAvailable = true
			}
			project.sessions++
			if !costAvailable {
				project.costUnknown = true
			}
		}

		// Time-of-day slot aggregation.
		slot := agg.slots[hour]
		slot.totalTokens += int64(input + output + reasoning + cacheRead + cacheWrite)
		slot.inputTokens += int64(input)
		slot.outputTokens += int64(output)
		slot.reasoning += int64(reasoning)
		slot.cacheRead += int64(cacheRead)
		slot.cacheCreate += int64(cacheWrite)
		slot.sessions++
		agg.slots[hour] = slot
	}

	return byDate
}

func openCodeProjectName(cwd string) string {
	raw := strings.TrimSpace(cwd)
	if raw == "" {
		return ""
	}
	name := filepath.Base(filepath.Clean(raw))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}
