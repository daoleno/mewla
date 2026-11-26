package translate

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

// DefaultAnthropicMaxTokens is the output cap sent when the agent gave none:
// Anthropic requires one, and Codex never sends it.
const DefaultAnthropicMaxTokens = 32000

// WriteAnthropic writes r as an Anthropic Messages request, always streamed.
func WriteAnthropic(r *Request) ([]byte, error) {
	out := map[string]any{"model": r.Model, "stream": true}
	maxTokens := r.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultAnthropicMaxTokens
	}
	msgs := anthropicMessages(pairToolCalls(r.Messages))
	thinking := r.Thinking && r.Effort != "none" && r.Effort != "minimal" && thinkingFits(msgs)
	if thinking {
		budget := r.ThinkingBudget
		if budget <= 0 {
			budget = budgetOfEffort(firstNonEmpty(r.Effort, "medium"))
		}
		if budget >= maxTokens {
			maxTokens = budget + 8192
		}
		out["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
	} else {
		// Without thinking on, Anthropic refuses thinking blocks with no
		// signature check failing; strip them all.
		for i := range msgs {
			msgs[i].Content = withoutThinking(msgs[i].Content)
		}
		if r.Temperature != nil {
			out["temperature"] = *r.Temperature
		}
		if r.TopP != nil {
			out["top_p"] = *r.TopP
		}
	}
	out["max_tokens"] = maxTokens
	var system []map[string]any
	for _, p := range r.System {
		if p.Kind == PartText && p.Text != "" {
			system = append(system, anthropicTextBlock(p))
		}
	}
	var tools []map[string]any
	for _, t := range r.Tools {
		tools = append(tools, map[string]any{"name": anthropicName(t.Name), "description": t.Description, "input_schema": schemaOrEmpty(t.Schema)})
	}
	markCache(system, tools, msgs)
	if len(system) > 0 {
		out["system"] = system
	}
	out["messages"] = msgs
	if len(tools) > 0 {
		out["tools"] = tools
		choice := map[string]any{"type": "auto"}
		switch r.Choice.Mode {
		case ChoiceNone:
			choice = map[string]any{"type": "none"}
		case ChoiceAny:
			choice = map[string]any{"type": "any"}
		case ChoiceTool:
			choice = map[string]any{"type": "tool", "name": anthropicName(r.Choice.Name)}
		}
		if r.Parallel != nil && !*r.Parallel && choice["type"] != "none" {
			choice["disable_parallel_tool_use"] = true
		}
		if thinking && (choice["type"] == "any" || choice["type"] == "tool") {
			// Anthropic takes only auto or none with thinking on.
			choice["type"] = "auto"
			delete(choice, "name")
		}
		out["tool_choice"] = choice
	}
	if len(r.Stop) > 0 {
		out["stop_sequences"] = r.Stop
	}
	if r.User != "" {
		out["metadata"] = map[string]string{"user_id": r.User}
	}
	return json.Marshal(out)
}

type anthropicMsg struct {
	Role    string           `json:"role"`
	Content []map[string]any `json:"content"`
}

func anthropicMessages(msgs []Message) []anthropicMsg {
	var out []anthropicMsg
	for _, m := range msgs {
		var blocks, thinking []map[string]any
		for _, p := range m.Parts {
			b := anthropicBlockOf(p, m.Role)
			if b == nil {
				continue
			}
			if p.Kind == PartThinking {
				thinking = append(thinking, b)
				continue
			}
			blocks = append(blocks, b)
		}
		// An assistant's thinking comes before what it said and called.
		blocks = append(thinking, blocks...)
		if len(blocks) == 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == m.Role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			continue
		}
		out = append(out, anthropicMsg{Role: m.Role, Content: blocks})
	}
	if len(out) > 0 && out[0].Role == RoleAssistant {
		out = append([]anthropicMsg{{Role: RoleUser, Content: []map[string]any{{"type": "text", "text": "(The conversation so far follows.)"}}}}, out...)
	}
	return out
}

func anthropicBlockOf(p Part, role string) map[string]any {
	var b map[string]any
	switch p.Kind {
	case PartText:
		if strings.TrimSpace(p.Text) == "" {
			return nil
		}
		b = anthropicTextBlock(p)
	case PartImage, PartFile:
		typ := "image"
		if p.Kind == PartFile {
			typ = "document"
		}
		switch {
		case p.Data != "":
			b = map[string]any{"type": typ, "source": map[string]string{"type": "base64", "media_type": p.MediaType, "data": p.Data}}
		case p.URL != "":
			b = map[string]any{"type": typ, "source": map[string]string{"type": "url", "url": p.URL}}
		default:
			return nil
		}
	case PartToolCall:
		var input any
		if err := json.Unmarshal([]byte(p.Args), &input); err != nil {
			input = map[string]string{"input": p.Args}
		}
		if _, isObject := input.(map[string]any); !isObject {
			input = map[string]any{"input": input}
		}
		b = map[string]any{"type": "tool_use", "id": anthropicName(p.CallID), "name": anthropicName(p.Name), "input": input}
	case PartToolResult:
		var content []map[string]any
		for _, r := range p.Result {
			if blk := anthropicBlockOf(r, role); blk != nil {
				content = append(content, blk)
			}
		}
		b = map[string]any{"type": "tool_result", "tool_use_id": anthropicName(p.CallID)}
		if len(content) > 0 {
			b["content"] = content
		}
		if p.IsError {
			b["is_error"] = true
		}
	case PartThinking:
		switch {
		case p.Redacted != "":
			return map[string]any{"type": "redacted_thinking", "data": p.Redacted}
		case p.Signature != "":
			return map[string]any{"type": "thinking", "thinking": p.Text, "signature": p.Signature}
		}
		// Thinking from another API carries no Anthropic signature, which
		// Anthropic refuses: it is left out.
		return nil
	default:
		return nil
	}
	if p.Cache != nil {
		b["cache_control"] = cacheControl(p.Cache)
	}
	return b
}

func anthropicTextBlock(p Part) map[string]any {
	b := map[string]any{"type": "text", "text": p.Text}
	if p.Cache != nil {
		b["cache_control"] = cacheControl(p.Cache)
	}
	return b
}

func cacheControl(c *CacheMark) map[string]string {
	m := map[string]string{"type": "ephemeral"}
	if c.TTL != "" {
		m["ttl"] = c.TTL
	}
	return m
}

// markCache adds prompt-cache breakpoints when the request carries none (a
// request read from another API never does): after the tools, after the
// system prompt and after the newest message. Anthropic then bills the
// unchanged prefix of each turn at the cache price instead of in full.
func markCache(system, tools []map[string]any, msgs []anthropicMsg) {
	for _, group := range [][]map[string]any{system, tools} {
		for _, b := range group {
			if _, ok := b["cache_control"]; ok {
				return
			}
		}
	}
	for _, m := range msgs {
		for _, b := range m.Content {
			if _, ok := b["cache_control"]; ok {
				return
			}
		}
	}
	mark := func(b map[string]any) { b["cache_control"] = map[string]string{"type": "ephemeral"} }
	if len(tools) > 0 {
		mark(tools[len(tools)-1])
	}
	if len(system) > 0 {
		mark(system[len(system)-1])
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		content := msgs[i].Content
		for j := len(content) - 1; j >= 0; j-- {
			if t := content[j]["type"]; t != "thinking" && t != "redacted_thinking" {
				mark(content[j])
				return
			}
		}
	}
}

// thinkingFits says whether thinking may be turned on: Anthropic requires the
// newest assistant turn that called a tool to begin with its thinking block,
// and a conversation from another API (or from a turn without thinking) may
// not have one.
func thinkingFits(msgs []anthropicMsg) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != RoleAssistant {
			continue
		}
		calls := false
		for _, b := range msgs[i].Content {
			if b["type"] == "tool_use" {
				calls = true
			}
		}
		if !calls {
			return true
		}
		t := msgs[i].Content[0]["type"]
		return t == "thinking" || t == "redacted_thinking"
	}
	return true
}

func withoutThinking(blocks []map[string]any) []map[string]any {
	out := blocks[:0]
	for _, b := range blocks {
		if t := b["type"]; t != "thinking" && t != "redacted_thinking" {
			out = append(out, b)
		}
	}
	return out
}

var notAnthropicName = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// anthropicName fits a tool name or call id to what Anthropic takes.
func anthropicName(s string) string {
	s = notAnthropicName.ReplaceAllString(s, "_")
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Data string `json:"data"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
	Error *upstreamBody   `json:"error"`
}

type anthropicUsage struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
}

func (u anthropicUsage) usage() Usage {
	return Usage{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
}

// DecodeAnthropic reads an Anthropic Messages reply, streamed or whole, as
// events.
func DecodeAnthropic(body io.Reader, contentType string, emit func(Event)) error {
	if !strings.Contains(contentType, "event-stream") {
		raw, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		var whole struct {
			Content []anthropicBlock `json:"content"`
			Stop    string           `json:"stop_reason"`
			Usage   anthropicUsage   `json:"usage"`
			Error   *upstreamBody    `json:"error"`
		}
		if err := json.Unmarshal(raw, &whole); err != nil {
			return err
		}
		if whole.Error != nil {
			emit(Event{Kind: EvError, Err: whole.Error.toError(0)})
			return nil
		}
		for i, b := range whole.Content {
			switch b.Type {
			case "text":
				emit(Event{Kind: EvText, Index: i, Text: b.Text})
			case "thinking":
				emit(Event{Kind: EvThinking, Index: i, Text: b.Thinking})
				emit(Event{Kind: EvSignature, Index: i, Text: b.Signature})
			case "redacted_thinking":
				emit(Event{Kind: EvRedacted, Index: i, Text: b.Data})
			case "tool_use":
				emit(Event{Kind: EvToolStart, Index: i, CallID: b.ID, Name: b.Name})
				emit(Event{Kind: EvToolArgs, Index: i, Text: string(b.Input)})
			}
		}
		emit(Event{Kind: EvUsage, Usage: whole.Usage.usage()})
		emit(Event{Kind: EvStop, Stop: anthropicStop(whole.Stop)})
		return nil
	}
	sse := newSSEReader(body)
	stopped := false
	for {
		ev, err := sse.next()
		if errors.Is(err, io.EOF) {
			if !stopped {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if err != nil {
			return err
		}
		var e anthropicStreamEvent
		if json.Unmarshal(ev.Data, &e) != nil {
			continue
		}
		switch e.Type {
		case "message_start":
			if e.Message != nil {
				emit(Event{Kind: EvUsage, Usage: e.Message.Usage.usage()})
			}
		case "content_block_start":
			if b := e.ContentBlock; b != nil {
				switch b.Type {
				case "tool_use":
					emit(Event{Kind: EvToolStart, Index: e.Index, CallID: b.ID, Name: b.Name})
				case "redacted_thinking":
					emit(Event{Kind: EvRedacted, Index: e.Index, Text: b.Data})
				case "text":
					if b.Text != "" {
						emit(Event{Kind: EvText, Index: e.Index, Text: b.Text})
					}
				}
			}
		case "content_block_delta":
			if d := e.Delta; d != nil {
				switch d.Type {
				case "text_delta":
					emit(Event{Kind: EvText, Index: e.Index, Text: d.Text})
				case "thinking_delta":
					emit(Event{Kind: EvThinking, Index: e.Index, Text: d.Thinking})
				case "signature_delta":
					emit(Event{Kind: EvSignature, Index: e.Index, Text: d.Signature})
				case "input_json_delta":
					emit(Event{Kind: EvToolArgs, Index: e.Index, Text: d.PartialJSON})
				}
			}
		case "message_delta":
			if e.Usage != nil {
				emit(Event{Kind: EvUsage, Usage: e.Usage.usage()})
			}
			if e.Delta != nil && e.Delta.StopReason != "" {
				stopped = true
				emit(Event{Kind: EvStop, Stop: anthropicStop(e.Delta.StopReason)})
			}
		case "message_stop":
			if !stopped {
				stopped = true
				emit(Event{Kind: EvStop, Stop: StopEnd})
			}
			return nil
		case "error":
			if e.Error != nil {
				emit(Event{Kind: EvError, Err: e.Error.toError(0)})
			}
			return nil
		}
	}
}

func anthropicStop(reason string) string {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return StopLength
	case "tool_use":
		return StopTool
	case "refusal":
		return StopRefusal
	}
	return StopEnd
}
