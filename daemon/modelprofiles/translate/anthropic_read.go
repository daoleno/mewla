package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *anthropicSrc   `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
	Title     string          `json:"title,omitempty"`
	Cache     *anthropicCache `json:"cache_control,omitempty"`
}

type anthropicSrc struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicCache struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type anthropicRequest struct {
	Model    string          `json:"model"`
	System   json.RawMessage `json:"system,omitempty"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Type        string          `json:"type,omitempty"`
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"input_schema,omitempty"`
	} `json:"tools,omitempty"`
	ToolChoice *struct {
		Type                   string `json:"type"`
		Name                   string `json:"name,omitempty"`
		DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
	} `json:"tool_choice,omitempty"`
	MaxTokens     int      `json:"max_tokens"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
	Stream        bool     `json:"stream,omitempty"`
	Thinking      *struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens,omitempty"`
	} `json:"thinking,omitempty"`
	OutputConfig *struct {
		Effort string `json:"effort,omitempty"`
	} `json:"output_config,omitempty"`
	Metadata *struct {
		UserID string `json:"user_id,omitempty"`
	} `json:"metadata,omitempty"`
}

// ReadAnthropic reads an Anthropic Messages request.
func ReadAnthropic(body []byte) (*Request, error) {
	var in anthropicRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	req := &Request{
		Model: in.Model, MaxTokens: in.MaxTokens, Temperature: in.Temperature, TopP: in.TopP,
		Stop: in.StopSequences, Stream: in.Stream,
	}
	system, err := anthropicContent(in.System)
	if err != nil {
		return nil, fmt.Errorf("anthropic system: %w", err)
	}
	for _, b := range system {
		if b.Type == "text" {
			// Claude Code's billing line is for Anthropic's Messages API
			// alone, and it changes every turn at the very start of the
			// prompt, where it would spoil the upstream's cache.
			if text := strings.TrimLeft(b.Text, " \t\r\n"); strings.HasPrefix(text, "x-anthropic-billing-header:") {
				_, rest, _ := strings.Cut(text, "\n")
				if b.Text = strings.TrimLeft(rest, "\r\n"); strings.TrimSpace(b.Text) == "" {
					continue
				}
			}
			req.System = append(req.System, Part{Kind: PartText, Text: b.Text, Cache: cacheMark(b.Cache)})
		}
	}
	for i, m := range in.Messages {
		blocks, err := anthropicContent(m.Content)
		if err != nil {
			return nil, fmt.Errorf("anthropic message %d: %w", i, err)
		}
		msg := Message{Role: m.Role}
		for _, b := range blocks {
			if part, ok := anthropicPart(b); ok {
				msg.Parts = append(msg.Parts, part)
			}
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, t := range in.Tools {
		// Server tools (web search, code execution, …) run at Anthropic;
		// no other API runs them.
		if t.Type != "" && t.Type != "custom" {
			continue
		}
		req.Tools = append(req.Tools, Tool{Name: t.Name, Description: t.Description, Schema: t.InputSchema})
	}
	if c := in.ToolChoice; c != nil {
		switch c.Type {
		case "any":
			req.Choice.Mode = ChoiceAny
		case "tool":
			req.Choice = ToolChoice{Mode: ChoiceTool, Name: c.Name}
		case "none":
			req.Choice.Mode = ChoiceNone
		}
		if c.DisableParallelToolUse {
			no := false
			req.Parallel = &no
		}
	}
	if t := in.Thinking; t != nil && (t.Type == "enabled" || t.Type == "adaptive") {
		req.Thinking = true
		req.ThinkingBudget = t.BudgetTokens
		req.Effort = effortOfBudget(t.BudgetTokens)
	}
	if in.OutputConfig != nil && in.OutputConfig.Effort != "" {
		req.Effort = in.OutputConfig.Effort
	}
	if in.Metadata != nil {
		req.User = in.Metadata.UserID
	}
	return req, nil
}

// anthropicContent reads a content field: a string or a list of blocks.
func anthropicContent(raw json.RawMessage) ([]anthropicBlock, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []anthropicBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []anthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func anthropicPart(b anthropicBlock) (Part, bool) {
	cache := cacheMark(b.Cache)
	switch b.Type {
	case "text":
		return Part{Kind: PartText, Text: b.Text, Cache: cache}, true
	case "image", "document":
		kind := PartImage
		if b.Type == "document" {
			kind = PartFile
		}
		if b.Source == nil {
			return Part{}, false
		}
		switch b.Source.Type {
		case "base64":
			return Part{Kind: kind, MediaType: b.Source.MediaType, Data: b.Source.Data, Filename: b.Title, Cache: cache}, true
		case "url":
			return Part{Kind: kind, URL: b.Source.URL, Filename: b.Title, Cache: cache}, true
		case "text":
			return Part{Kind: PartText, Text: b.Source.Data, Cache: cache}, true
		}
		return Part{}, false
	case "tool_use":
		args := strings.TrimSpace(string(b.Input))
		if args == "" || args == "null" {
			args = "{}"
		}
		return Part{Kind: PartToolCall, CallID: b.ID, Name: b.Name, Args: args, Cache: cache}, true
	case "tool_result":
		result := Part{Kind: PartToolResult, CallID: b.ToolUseID, IsError: b.IsError, Cache: cache}
		inner, err := anthropicContent(b.Content)
		if err == nil {
			for _, ib := range inner {
				if p, ok := anthropicPart(ib); ok && (p.Kind == PartText || p.Kind == PartImage) {
					p.Cache = nil
					result.Result = append(result.Result, p)
				}
			}
		}
		return result, true
	case "thinking":
		return Part{Kind: PartThinking, Text: b.Thinking, Signature: b.Signature}, true
	case "redacted_thinking":
		return Part{Kind: PartThinking, Redacted: b.Data}, true
	}
	// server_tool_use, web_search_tool_result and other server-side
	// blocks are Anthropic's own; another API cannot be shown them.
	return Part{}, false
}

func cacheMark(c *anthropicCache) *CacheMark {
	if c == nil || c.Type != "ephemeral" {
		return nil
	}
	return &CacheMark{TTL: c.TTL}
}

// Thinking budgets for each effort, and back. The steps follow Anthropic's
// guidance (a few thousand tokens for light thinking, tens of thousands for
// hard problems).
var effortBudgets = []struct {
	effort string
	budget int
}{
	{"minimal", 1024},
	{"low", 4096},
	{"medium", 10000},
	{"high", 24000},
	{"xhigh", 32000},
	{"max", 48000},
}

func budgetOfEffort(effort string) int {
	switch effort {
	case "", "none":
		return 0
	case "ultra":
		effort = "max"
	}
	for _, e := range effortBudgets {
		if e.effort == effort {
			return e.budget
		}
	}
	return 10000
}

func effortOfBudget(budget int) string {
	if budget <= 0 {
		return ""
	}
	for _, e := range effortBudgets {
		if budget <= e.budget {
			return e.effort
		}
	}
	return "max"
}
