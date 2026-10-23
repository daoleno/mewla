package codexctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Status is what Codex's own app-server says about one Worker's thread: the
// thread, the effective configuration, the signed-in account and its quota. It
// is what the phone's status sheet shows, so nothing in it is read off the
// terminal. A section the server would not answer is nil; the others stand.
type Status struct {
	Thread  *ThreadDetails  `json:"thread,omitempty"`
	Config  *ConfigDetails  `json:"config,omitempty"`
	Account *AccountDetails `json:"account,omitempty"`
	Limits  []LimitDetails  `json:"limits,omitempty"`
	Tokens  *TokenDetails   `json:"tokens,omitempty"`
}

type ThreadDetails struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	Model         string `json:"model,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	Effort        string `json:"effort,omitempty"`
	CLIVersion    string `json:"cli_version,omitempty"`
	State         string `json:"state,omitempty"`
	Path          string `json:"path,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

type ConfigDetails struct {
	Model         string `json:"model,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	Effort        string `json:"effort,omitempty"`
	Summary       string `json:"summary,omitempty"`
	ServiceTier   string `json:"service_tier,omitempty"`
	Approval      string `json:"approval,omitempty"`
	Sandbox       string `json:"sandbox,omitempty"`
	ContextWindow int64  `json:"context_window,omitempty"`
}

type AccountDetails struct {
	// Kind is chatgpt, apiKey or amazonBedrock.
	Kind  string `json:"kind"`
	Email string `json:"email,omitempty"`
	Plan  string `json:"plan,omitempty"`
}

type LimitDetails struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Plan      string          `json:"plan,omitempty"`
	Windows   []WindowDetails `json:"windows,omitempty"`
	Credits   *CreditDetails  `json:"credits,omitempty"`
	Reached   string          `json:"reached,omitempty"`
	SpendStop bool            `json:"spend_control_reached,omitempty"`
}

type WindowDetails struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes,omitempty"`
	ResetsAt      int64   `json:"resets_at,omitempty"`
}

type CreditDetails struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}

// TokenDetails is the thread's token use, which the server pushes after each
// turn and never serves on request.
type TokenDetails struct {
	Total         TokenCounts `json:"total"`
	Last          TokenCounts `json:"last"`
	ContextWindow int64       `json:"context_window,omitempty"`
}

type TokenCounts struct {
	Input     int64 `json:"input"`
	Cached    int64 `json:"cached"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Total     int64 `json:"total"`
}

// ParseTokenUsage reads the params of a thread/tokenUsage/updated
// notification.
func ParseTokenUsage(params json.RawMessage) (TokenDetails, bool) {
	var update struct {
		TokenUsage struct {
			Total         wireTokens `json:"total"`
			Last          wireTokens `json:"last"`
			ContextWindow *int64     `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(params, &update) != nil {
		return TokenDetails{}, false
	}
	details := TokenDetails{Total: update.TokenUsage.Total.counts(), Last: update.TokenUsage.Last.counts()}
	if update.TokenUsage.ContextWindow != nil {
		details.ContextWindow = *update.TokenUsage.ContextWindow
	}
	return details, true
}

type wireTokens struct {
	Input     int64 `json:"inputTokens"`
	Cached    int64 `json:"cachedInputTokens"`
	Output    int64 `json:"outputTokens"`
	Reasoning int64 `json:"reasoningOutputTokens"`
	Total     int64 `json:"totalTokens"`
}

func (w wireTokens) counts() TokenCounts {
	return TokenCounts{Input: w.Input, Cached: w.Cached, Output: w.Output, Reasoning: w.Reasoning, Total: w.Total}
}

// ReadStatus asks the app-server for a thread's status: thread/read,
// config/read, account/read and account/rateLimits/read, together. A call the
// server refuses leaves its section empty; the read fails only when none of
// them answered.
func (c *Client) ReadStatus(ctx context.Context, threadID, cwd string) (Status, error) {
	var (
		wg       sync.WaitGroup
		status   Status
		failures [4]error
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		raw, err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false})
		if err != nil {
			failures[0] = err
			return
		}
		status.Thread, failures[0] = parseThread(raw)
	}()
	go func() {
		defer wg.Done()
		params := map[string]any{"includeLayers": false}
		if cwd != "" {
			params["cwd"] = cwd
		}
		raw, err := c.call(ctx, "config/read", params)
		if err != nil {
			failures[1] = err
			return
		}
		status.Config, failures[1] = parseConfig(raw)
	}()
	go func() {
		defer wg.Done()
		raw, err := c.call(ctx, "account/read", map[string]any{"refreshToken": false})
		if err != nil {
			failures[2] = err
			return
		}
		status.Account, failures[2] = parseAccount(raw)
	}()
	go func() {
		defer wg.Done()
		raw, err := c.call(ctx, "account/rateLimits/read", nil)
		if err != nil {
			failures[3] = err
			return
		}
		status.Limits, failures[3] = parseLimits(raw)
	}()
	wg.Wait()
	if status.Thread == nil && status.Config == nil && status.Account == nil && len(status.Limits) == 0 {
		return Status{}, errors.Join(failures[:]...)
	}
	return status, nil
}

// ThreadPath is the rollout file of a thread, empty when it has none yet. A
// daemon that meets a running Codex learns its rollout this way, the thread's
// announcement having come before it connected.
func (c *Client) ThreadPath(ctx context.Context, threadID string) (string, error) {
	raw, err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false})
	if err != nil {
		return "", err
	}
	thread, err := parseThread(raw)
	if err != nil {
		return "", err
	}
	return thread.Path, nil
}

func parseThread(raw json.RawMessage) (*ThreadDetails, error) {
	var read struct {
		Thread struct {
			ID            string       `json:"id"`
			Name          *string      `json:"name"`
			Cwd           string       `json:"cwd"`
			Model         *string      `json:"model"`
			ModelProvider string       `json:"modelProvider"`
			Effort        *string      `json:"reasoningEffort"`
			CLIVersion    string       `json:"cliVersion"`
			Status        ThreadStatus `json:"status"`
			Path          *string      `json:"path"`
			CreatedAt     int64        `json:"createdAt"`
			UpdatedAt     int64        `json:"updatedAt"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, fmt.Errorf("%w: thread/read: %v", ErrProtocol, err)
	}
	t := read.Thread
	return &ThreadDetails{
		ID: t.ID, Name: text(t.Name), Cwd: t.Cwd, Model: text(t.Model), ModelProvider: t.ModelProvider,
		Effort: text(t.Effort), CLIVersion: t.CLIVersion, State: string(t.Status), Path: text(t.Path),
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}, nil
}

func parseConfig(raw json.RawMessage) (*ConfigDetails, error) {
	var read struct {
		Config struct {
			Model         *string         `json:"model"`
			ModelProvider *string         `json:"model_provider"`
			Effort        *string         `json:"model_reasoning_effort"`
			Summary       *string         `json:"model_reasoning_summary"`
			ServiceTier   *string         `json:"service_tier"`
			Approval      json.RawMessage `json:"approval_policy"`
			Sandbox       json.RawMessage `json:"sandbox_mode"`
			ContextWindow *int64          `json:"model_context_window"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, fmt.Errorf("%w: config/read: %v", ErrProtocol, err)
	}
	c := read.Config
	details := &ConfigDetails{
		Model: text(c.Model), ModelProvider: text(c.ModelProvider), Effort: text(c.Effort), Summary: text(c.Summary),
		ServiceTier: text(c.ServiceTier), Approval: setting(c.Approval), Sandbox: setting(c.Sandbox),
	}
	if c.ContextWindow != nil {
		details.ContextWindow = *c.ContextWindow
	}
	return details, nil
}

func parseAccount(raw json.RawMessage) (*AccountDetails, error) {
	var read struct {
		Account *struct {
			Type  string  `json:"type"`
			Email *string `json:"email"`
			Plan  *string `json:"planType"`
		} `json:"account"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, fmt.Errorf("%w: account/read: %v", ErrProtocol, err)
	}
	if read.Account == nil {
		return nil, nil
	}
	return &AccountDetails{Kind: read.Account.Type, Email: text(read.Account.Email), Plan: text(read.Account.Plan)}, nil
}

type wireSnapshot struct {
	LimitID   *string     `json:"limitId"`
	LimitName *string     `json:"limitName"`
	Plan      *string     `json:"planType"`
	Primary   *wireWindow `json:"primary"`
	Secondary *wireWindow `json:"secondary"`
	Credits   *struct {
		HasCredits bool    `json:"hasCredits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
	Reached   *string `json:"rateLimitReachedType"`
	SpendStop *bool   `json:"spendControlReached"`
}

func (s wireSnapshot) details() LimitDetails {
	out := LimitDetails{ID: text(s.LimitID), Name: text(s.LimitName), Plan: text(s.Plan), Reached: text(s.Reached)}
	for _, w := range []*wireWindow{s.Primary, s.Secondary} {
		if w == nil {
			continue
		}
		window := WindowDetails{UsedPercent: w.UsedPercent}
		if w.WindowDurationMins != nil {
			window.WindowMinutes = *w.WindowDurationMins
		}
		if w.ResetsAt != nil {
			window.ResetsAt = *w.ResetsAt
		}
		out.Windows = append(out.Windows, window)
	}
	if s.Credits != nil && (s.Credits.HasCredits || s.Credits.Unlimited) {
		out.Credits = &CreditDetails{HasCredits: s.Credits.HasCredits, Unlimited: s.Credits.Unlimited, Balance: text(s.Credits.Balance)}
	}
	out.SpendStop = s.SpendStop != nil && *s.SpendStop
	return out
}

// parseLimits lists every metered bucket the server names, the general one
// first; a server that names only the single historical bucket gives that one.
func parseLimits(raw json.RawMessage) ([]LimitDetails, error) {
	var read struct {
		RateLimits *wireSnapshot           `json:"rateLimits"`
		ByLimitID  map[string]wireSnapshot `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, fmt.Errorf("%w: account/rateLimits/read: %v", ErrProtocol, err)
	}
	var limits []LimitDetails
	if len(read.ByLimitID) == 0 {
		if read.RateLimits != nil {
			if limit := read.RateLimits.details(); len(limit.Windows) > 0 || limit.Credits != nil {
				limits = append(limits, limit)
			}
		}
		return limits, nil
	}
	ids := make([]string, 0, len(read.ByLimitID))
	for id := range read.ByLimitID {
		ids = append(ids, id)
	}
	sortLimitIDs(ids)
	for _, id := range ids {
		snapshot := read.ByLimitID[id]
		limit := snapshot.details()
		if limit.ID == "" {
			limit.ID = id
		}
		if len(limit.Windows) > 0 || limit.Credits != nil {
			limits = append(limits, limit)
		}
	}
	return limits, nil
}

// sortLimitIDs puts the general "codex" bucket first and the rest in name
// order, so the list does not shuffle between reads.
func sortLimitIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		if (ids[i] == "codex") != (ids[j] == "codex") {
			return ids[i] == "codex"
		}
		return ids[i] < ids[j]
	})
}

func text(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// setting names a configuration value that is a word, or a structured policy
// the sheet has no room for.
func setting(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var word string
	if json.Unmarshal(raw, &word) == nil {
		return word
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil && len(object) == 1 {
		for key := range object {
			return key
		}
	}
	return "custom"
}
