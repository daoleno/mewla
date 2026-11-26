package translate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// WriteChat writes r as an OpenAI Chat Completions request, always streamed.
func WriteChat(r *Request) ([]byte, error) {
	out := map[string]any{"model": r.Model, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	var msgs []map[string]any
	if sys := joinText(r.System); sys != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": sys})
	}
	for _, m := range pairToolCalls(r.Messages) {
		if m.Role == RoleAssistant {
			msgs = append(msgs, chatAssistant(m))
			continue
		}
		// A user turn's tool results come first, each its own tool
		// message right after the assistant's calls; what else the user
		// gave follows in one user message.
		var rest []Part
		var resultImages []Part
		for _, p := range m.Parts {
			if p.Kind != PartToolResult {
				rest = append(rest, p)
				continue
			}
			text, images := splitResult(p)
			if p.IsError && text != "" {
				text = "Error: " + text
			}
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": p.CallID, "content": text})
			resultImages = append(resultImages, images...)
		}
		if len(resultImages) > 0 {
			// Chat's tool messages take text only: the images a tool gave
			// are shown to the model in the user message after them.
			rest = append([]Part{{Kind: PartText, Text: "Images the tool returned:"}}, append(resultImages, rest...)...)
		}
		if content := chatContent(rest); content != nil {
			msgs = append(msgs, map[string]any{"role": "user", "content": content})
		}
	}
	out["messages"] = msgs
	if len(r.Tools) > 0 {
		var tools []map[string]any
		for _, t := range r.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": schemaOrEmpty(t.Schema),
			}})
		}
		out["tools"] = tools
		switch r.Choice.Mode {
		case ChoiceNone:
			out["tool_choice"] = "none"
		case ChoiceAny:
			out["tool_choice"] = "required"
		case ChoiceTool:
			out["tool_choice"] = map[string]any{"type": "function", "function": map[string]string{"name": r.Choice.Name}}
		}
		if r.Parallel != nil {
			out["parallel_tool_calls"] = *r.Parallel
		}
	}
	if r.MaxTokens > 0 {
		out["max_tokens"] = r.MaxTokens
	}
	if r.Temperature != nil {
		out["temperature"] = *r.Temperature
	}
	if r.TopP != nil {
		out["top_p"] = *r.TopP
	}
	if len(r.Stop) > 0 {
		out["stop"] = r.Stop
	}
	if e := chatEffort(r.Effort); e != "" {
		out["reasoning_effort"] = e
	}
	if r.User != "" {
		out["user"] = r.User
	}
	return json.Marshal(out)
}

// chatEffort is the reasoning_effort Chat APIs take (low, medium, high).
func chatEffort(effort string) string {
	switch effort {
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh", "max", "ultra":
		return "high"
	}
	return ""
}

func chatAssistant(m Message) map[string]any {
	msg := map[string]any{"role": "assistant"}
	var text, thinking strings.Builder
	var calls []map[string]any
	for _, p := range m.Parts {
		switch p.Kind {
		case PartText:
			text.WriteString(p.Text)
		case PartThinking:
			thinking.WriteString(p.Text)
		case PartToolCall:
			calls = append(calls, map[string]any{"id": p.CallID, "type": "function", "function": map[string]string{"name": p.Name, "arguments": p.Args}})
		}
	}
	msg["content"] = text.String()
	if len(calls) > 0 {
		msg["tool_calls"] = calls
		if text.Len() == 0 {
			msg["content"] = nil
		}
	}
	// APIs that think aloud (DeepSeek, Kimi, GLM) want their reasoning back
	// with the calls it led to; others ignore the field.
	if thinking.Len() > 0 {
		msg["reasoning_content"] = thinking.String()
	}
	return msg
}

// chatContent is a user message's content: a string for text alone, else a
// list of text and image parts. A file other APIs could read is named in
// text: Chat Completions takes images only.
func chatContent(parts []Part) any {
	var list []map[string]any
	onlyText := true
	var text strings.Builder
	for _, p := range parts {
		switch p.Kind {
		case PartText:
			if p.Text == "" {
				continue
			}
			list = append(list, map[string]any{"type": "text", "text": p.Text})
			text.WriteString(p.Text)
		case PartImage:
			onlyText = false
			url := p.URL
			if p.Data != "" {
				url = dataURL(p.MediaType, p.Data)
			}
			list = append(list, map[string]any{"type": "image_url", "image_url": map[string]string{"url": url}})
		case PartFile:
			name := firstNonEmpty(p.Filename, p.MediaType, "a file")
			note := "[" + name + " is attached, but this Provider's API takes no files]"
			list = append(list, map[string]any{"type": "text", "text": note})
			text.WriteString(note)
		}
	}
	if len(list) == 0 {
		return nil
	}
	if onlyText {
		return text.String()
	}
	return list
}

func splitResult(p Part) (string, []Part) {
	var text strings.Builder
	var images []Part
	for _, r := range p.Result {
		switch r.Kind {
		case PartText:
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(r.Text)
		case PartImage:
			images = append(images, r)
		}
	}
	return text.String(), images
}

// pairToolCalls mends a conversation so every tool call has its result next
// and every result answers a call, as Chat and Anthropic both demand. A call
// left unanswered (the turn was interrupted) gets a result that says so; a
// result whose call is gone becomes plain text.
func pairToolCalls(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))
	open := map[string]bool{}
	var pending []string
	closePending := func() {
		if len(pending) == 0 {
			return
		}
		var parts []Part
		for _, id := range pending {
			if open[id] {
				parts = append(parts, Part{Kind: PartToolResult, CallID: id, IsError: true, Result: []Part{{Kind: PartText, Text: "No result: the call was interrupted."}}})
				delete(open, id)
			}
		}
		pending = nil
		if len(parts) > 0 {
			out = append(out, Message{Role: RoleUser, Parts: parts})
		}
	}
	for _, m := range msgs {
		if m.Role == RoleAssistant {
			closePending()
			for _, p := range m.Parts {
				if p.Kind == PartToolCall {
					open[p.CallID] = true
					pending = append(pending, p.CallID)
				}
			}
			out = append(out, m)
			continue
		}
		// Answers go in the order of the calls, an unanswered call gets
		// an error, and an answer to no call becomes plain text.
		given := map[string]Part{}
		var rest []Part
		for _, p := range m.Parts {
			if p.Kind != PartToolResult {
				rest = append(rest, p)
				continue
			}
			if open[p.CallID] {
				if _, dup := given[p.CallID]; !dup {
					given[p.CallID] = p
				}
				continue
			}
			text, _ := splitResult(p)
			rest = append(rest, Part{Kind: PartText, Text: "Tool result: " + text})
		}
		var results []Part
		for _, id := range pending {
			if !open[id] {
				continue
			}
			p, ok := given[id]
			if !ok {
				p = Part{Kind: PartToolResult, CallID: id, IsError: true, Result: []Part{{Kind: PartText, Text: "No result: the call was interrupted."}}}
			}
			results = append(results, p)
			delete(open, id)
		}
		pending = nil
		if parts := append(results, rest...); len(parts) > 0 {
			out = append(out, Message{Role: RoleUser, Parts: parts})
		}
	}
	return out
}

type chatChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta *struct {
			Content          *string `json:"content"`
			ReasoningContent *string `json:"reasoning_content"`
			Reasoning        *string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Message *struct {
			Content          *string `json:"content"`
			ReasoningContent *string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage    `json:"usage"`
	Error *upstreamBody `json:"error"`
}

type chatUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Prompt2    *struct {
		Cached int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	Completion2 *struct {
		Reasoning int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	CacheHit int `json:"prompt_cache_hit_tokens"`
}

func (u chatUsage) usage() Usage {
	cached := u.CacheHit
	if u.Prompt2 != nil && u.Prompt2.Cached > cached {
		cached = u.Prompt2.Cached
	}
	out := Usage{Input: max(u.Prompt-cached, 0), Output: u.Completion, CacheRead: cached}
	if u.Completion2 != nil {
		out.Reasoning = u.Completion2.Reasoning
	}
	return out
}

// DecodeChat reads a Chat Completions reply, streamed or whole, as events.
func DecodeChat(body io.Reader, contentType string, emit func(Event)) error {
	if !strings.Contains(contentType, "event-stream") {
		raw, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		var c chatChunk
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		if c.Error != nil {
			emit(Event{Kind: EvError, Err: c.Error.toError(0)})
			return nil
		}
		for _, ch := range c.Choices {
			if ch.Index != 0 || ch.Message == nil {
				continue
			}
			if s := ch.Message.ReasoningContent; s != nil && *s != "" {
				emit(Event{Kind: EvThinking, Text: *s})
			}
			if s := ch.Message.Content; s != nil && *s != "" {
				emit(Event{Kind: EvText, Text: *s})
			}
			for i, tc := range ch.Message.ToolCalls {
				emit(Event{Kind: EvToolStart, Index: i, CallID: tc.ID, Name: tc.Function.Name})
				emit(Event{Kind: EvToolArgs, Index: i, Text: tc.Function.Arguments})
			}
			if ch.FinishReason != nil {
				emit(Event{Kind: EvStop, Stop: chatStop(*ch.FinishReason)})
			}
		}
		if c.Usage != nil {
			emit(Event{Kind: EvUsage, Usage: c.Usage.usage()})
		}
		return nil
	}
	sse := newSSEReader(body)
	started := map[int]bool{}
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
		data := bytes.TrimSpace(ev.Data)
		if string(data) == "[DONE]" {
			if !stopped {
				emit(Event{Kind: EvStop, Stop: StopEnd})
			}
			return nil
		}
		var c chatChunk
		if json.Unmarshal(data, &c) != nil {
			continue
		}
		if c.Error != nil {
			emit(Event{Kind: EvError, Err: c.Error.toError(0)})
			return nil
		}
		for _, ch := range c.Choices {
			if ch.Index != 0 {
				continue
			}
			if d := ch.Delta; d != nil {
				for _, s := range []*string{d.ReasoningContent, d.Reasoning} {
					if s != nil && *s != "" {
						emit(Event{Kind: EvThinking, Text: *s})
						break
					}
				}
				if d.Content != nil && *d.Content != "" {
					emit(Event{Kind: EvText, Text: *d.Content})
				}
				for _, tc := range d.ToolCalls {
					if !started[tc.Index] {
						started[tc.Index] = true
						emit(Event{Kind: EvToolStart, Index: tc.Index, CallID: tc.ID, Name: tc.Function.Name})
					}
					if tc.Function.Arguments != "" {
						emit(Event{Kind: EvToolArgs, Index: tc.Index, Text: tc.Function.Arguments})
					}
				}
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				stopped = true
				emit(Event{Kind: EvStop, Stop: chatStop(*ch.FinishReason)})
			}
		}
		if c.Usage != nil {
			emit(Event{Kind: EvUsage, Usage: c.Usage.usage()})
		}
	}
}

func chatStop(reason string) string {
	switch reason {
	case "length":
		return StopLength
	case "tool_calls", "function_call":
		return StopTool
	case "content_filter", "sensitive":
		return StopRefusal
	}
	return StopEnd
}

func joinText(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind != PartText || p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

func schemaOrEmpty(s json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(s)) == 0 || string(bytes.TrimSpace(s)) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
