package translate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// toolCall is a tool call being gathered from its streamed pieces.
type toolCall struct {
	index  int
	callID string
	name   string
	args   strings.Builder
}

func (t *toolCall) arguments() string {
	args := strings.TrimSpace(t.args.String())
	if args == "" || !json.Valid([]byte(args)) {
		if args != "" {
			// A cut-off or invalid argument string is passed as text, so
			// the agent's tool reports the problem instead of the turn
			// failing here.
			b, _ := json.Marshal(map[string]string{"input": args})
			return string(b)
		}
		return "{}"
	}
	return args
}

// pendingTools gathers tool calls by stream index, in the order they began.
type pendingTools struct {
	order []*toolCall
	byIdx map[int]*toolCall
}

func (p *pendingTools) start(e Event) {
	if p.byIdx == nil {
		p.byIdx = map[int]*toolCall{}
	}
	if t, ok := p.byIdx[e.Index]; ok {
		// A repeated start fills in what the first lacked.
		t.callID = firstNonEmpty(t.callID, e.CallID)
		t.name = firstNonEmpty(t.name, e.Name)
		return
	}
	t := &toolCall{index: e.Index, callID: e.CallID, name: e.Name}
	if t.callID == "" {
		t.callID = "call_" + randomID()
	}
	p.byIdx[e.Index] = t
	p.order = append(p.order, t)
}

func (p *pendingTools) args(e Event) {
	if t, ok := p.byIdx[e.Index]; ok {
		t.args.WriteString(e.Text)
	}
}

func (p *pendingTools) take() []*toolCall {
	out := p.order
	p.order, p.byIdx = nil, nil
	return out
}

func randomID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// anthropicEncoder writes events as an Anthropic Messages reply.
type anthropicEncoder struct {
	model  string
	id     string
	stream bool
	sse    *sseWriter
	w      http.ResponseWriter

	started   bool
	blockOpen bool
	blockType string
	blocks    int
	content   []map[string]any // whole reply
	tools     pendingTools
	usage     Usage
	stop      string
}

func newAnthropicEncoder(w http.ResponseWriter, model string, stream bool) *anthropicEncoder {
	return &anthropicEncoder{w: w, model: model, stream: stream, id: "msg_" + randomID()}
}

func (a *anthropicEncoder) begin() {
	if a.started {
		return
	}
	a.started = true
	if !a.stream {
		return
	}
	a.w.Header().Set("Content-Type", "text/event-stream")
	a.w.Header().Set("Cache-Control", "no-cache")
	a.w.WriteHeader(http.StatusOK)
	a.sse = newSSEWriter(a.w)
	a.sse.event("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": a.id, "type": "message", "role": "assistant", "model": a.model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
	}})
}

func (a *anthropicEncoder) open(typ string, block map[string]any) {
	a.close()
	a.blockOpen, a.blockType = true, typ
	if a.stream {
		a.sse.event("content_block_start", map[string]any{"type": "content_block_start", "index": a.blocks, "content_block": block})
	}
	a.content = append(a.content, block)
}

func (a *anthropicEncoder) delta(delta map[string]any) {
	if a.stream {
		a.sse.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": a.blocks, "delta": delta})
	}
	last := a.content[len(a.content)-1]
	switch delta["type"] {
	case "text_delta":
		last["text"] = last["text"].(string) + delta["text"].(string)
	case "thinking_delta":
		last["thinking"] = last["thinking"].(string) + delta["thinking"].(string)
	case "signature_delta":
		last["signature"] = last["signature"].(string) + delta["signature"].(string)
	}
}

func (a *anthropicEncoder) close() {
	if !a.blockOpen {
		return
	}
	// A Chat model's reasoning has no Anthropic signature. It gets Mewla's
	// own, which Claude Code keeps and sends back, so the block can be told
	// apart and left out on the way to an endpoint that checks signatures
	// (NativeHistory).
	if a.blockType == "thinking" {
		if last := a.content[len(a.content)-1]; last["signature"] == "" {
			a.delta(map[string]any{"type": "signature_delta", "signature": UnsignedThinkingSignature})
		}
	}
	if a.stream {
		a.sse.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": a.blocks})
	}
	a.blockOpen = false
	a.blocks++
}

func (a *anthropicEncoder) flushTools() {
	for _, t := range a.tools.take() {
		args := t.arguments()
		a.open("tool_use", map[string]any{"type": "tool_use", "id": anthropicName(t.callID), "name": t.name, "input": map[string]any{}})
		if a.stream {
			a.sse.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": a.blocks, "delta": map[string]any{"type": "input_json_delta", "partial_json": args}})
		}
		a.content[len(a.content)-1]["input"] = json.RawMessage(args)
		a.close()
	}
}

func (a *anthropicEncoder) handle(e Event) {
	switch e.Kind {
	case EvUsage:
		a.usage.add(e.Usage)
		return
	case EvToolArgs:
		a.tools.args(e)
		return
	}
	a.begin()
	switch e.Kind {
	case EvText:
		if e.Text == "" {
			return
		}
		a.flushTools()
		if !a.blockOpen || a.blockType != "text" {
			a.open("text", map[string]any{"type": "text", "text": ""})
		}
		a.delta(map[string]any{"type": "text_delta", "text": e.Text})
	case EvThinking:
		if e.Text == "" {
			return
		}
		a.flushTools()
		if !a.blockOpen || a.blockType != "thinking" {
			a.open("thinking", map[string]any{"type": "thinking", "thinking": "", "signature": ""})
		}
		a.delta(map[string]any{"type": "thinking_delta", "thinking": e.Text})
	case EvSignature:
		if a.blockOpen && a.blockType == "thinking" && e.Text != "" {
			a.delta(map[string]any{"type": "signature_delta", "signature": e.Text})
		}
	case EvRedacted:
		a.flushTools()
		a.open("redacted_thinking", map[string]any{"type": "redacted_thinking", "data": e.Text})
		a.close()
	case EvToolStart:
		a.close()
		a.tools.start(e)
	case EvStop:
		a.stop = e.Stop
	}
}

func (a *anthropicEncoder) finish() {
	a.begin()
	a.flushTools()
	a.close()
	stop := map[string]string{StopEnd: "end_turn", StopLength: "max_tokens", StopTool: "tool_use", StopRefusal: "refusal"}[a.stop]
	if stop == "" {
		stop = "end_turn"
	}
	usage := map[string]int{
		"input_tokens": a.usage.Input, "output_tokens": a.usage.Output,
		"cache_read_input_tokens": a.usage.CacheRead, "cache_creation_input_tokens": a.usage.CacheWrite,
	}
	if !a.stream {
		if a.content == nil {
			a.content = []map[string]any{}
		}
		body, _ := json.Marshal(map[string]any{
			"id": a.id, "type": "message", "role": "assistant", "model": a.model, "content": a.content,
			"stop_reason": stop, "stop_sequence": nil, "usage": usage,
		})
		a.w.Header().Set("Content-Type", "application/json")
		a.w.WriteHeader(http.StatusOK)
		_, _ = a.w.Write(body)
		return
	}
	a.sse.event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": usage})
	a.sse.event("message_stop", map[string]any{"type": "message_stop"})
}

// fail ends the reply with an error. Before anything was sent the error is
// the HTTP answer; afterwards it is an error event in the stream.
func (a *anthropicEncoder) fail(e *Error) {
	if !a.started || !a.stream {
		status, body := AnthropicError(e)
		a.w.Header().Set("Content-Type", "application/json")
		a.w.WriteHeader(status)
		_, _ = a.w.Write(body)
		a.started = true
		return
	}
	_, body := AnthropicError(e)
	a.sse.event("error", json.RawMessage(body))
}

func (a *anthropicEncoder) keepAlive() {
	if a.started && a.stream {
		a.sse.event("ping", map[string]string{"type": "ping"})
	}
}

func (a *anthropicEncoder) began() bool { return a.started }
