package translate

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// toolNames maps the flat tool names other APIs were offered back to what
// Codex asked for: a namespaced function, or a freeform (custom) tool.
type toolNames map[string]struct {
	name, namespace string
	custom          bool
}

func toolNamesOf(body []byte) toolNames {
	var in struct {
		Tools []responsesTool `json:"tools"`
	}
	_ = json.Unmarshal(body, &in)
	names := toolNames{}
	for _, t := range in.Tools {
		switch t.Type {
		case "custom":
			names[t.Name] = struct {
				name, namespace string
				custom          bool
			}{t.Name, "", true}
		case "namespace":
			for _, nt := range t.Tools {
				names[t.Name+namespaceSep+nt.Name] = struct {
					name, namespace string
					custom          bool
				}{nt.Name, t.Name, false}
			}
		}
	}
	return names
}

// responsesEncoder writes events as an OpenAI Responses reply.
type responsesEncoder struct {
	model  string
	id     string
	stream bool
	seal   bool // seal Anthropic thinking into encrypted_content
	names  toolNames
	sse    *sseWriter
	w      http.ResponseWriter

	started bool
	created int64
	output  []map[string]any

	// the item being streamed: a message or a reasoning item
	open     string
	openItem map[string]any
	text     strings.Builder
	thinking strings.Builder
	sig      strings.Builder
	redacted string

	tools pendingTools
	usage Usage
	stop  string
}

func newResponsesEncoder(w http.ResponseWriter, model string, stream, seal bool, names toolNames) *responsesEncoder {
	return &responsesEncoder{w: w, model: model, stream: stream, seal: seal, names: names, id: "resp_" + randomID(), created: time.Now().Unix()}
}

func (r *responsesEncoder) response(status string) map[string]any {
	resp := map[string]any{
		"id": r.id, "object": "response", "created_at": r.created, "status": status, "model": r.model,
		"output": r.outputOrEmpty(),
	}
	return resp
}

func (r *responsesEncoder) outputOrEmpty() []map[string]any {
	if r.output == nil {
		return []map[string]any{}
	}
	return r.output
}

func (r *responsesEncoder) begin() {
	if r.started {
		return
	}
	r.started = true
	if !r.stream {
		return
	}
	r.w.Header().Set("Content-Type", "text/event-stream")
	r.w.Header().Set("Cache-Control", "no-cache")
	r.w.WriteHeader(http.StatusOK)
	r.sse = newSSEWriter(r.w)
	r.send("response.created", map[string]any{"response": r.response("in_progress")})
	r.send("response.in_progress", map[string]any{"response": r.response("in_progress")})
}

func (r *responsesEncoder) send(typ string, fields map[string]any) {
	if !r.stream {
		return
	}
	fields["type"] = typ
	r.sse.event(typ, fields)
}

func (r *responsesEncoder) openMessage() {
	if r.open == "message" {
		return
	}
	r.closeOpen()
	r.open = "message"
	r.openItem = map[string]any{"id": "msg_" + randomID(), "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
	idx := len(r.output)
	r.send("response.output_item.added", map[string]any{"output_index": idx, "item": r.openItem})
	r.send("response.content_part.added", map[string]any{"item_id": r.openItem["id"], "output_index": idx, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
}

func (r *responsesEncoder) openReasoning() {
	if r.open == "reasoning" {
		return
	}
	r.closeOpen()
	r.open = "reasoning"
	r.openItem = map[string]any{"id": "rs_" + randomID(), "type": "reasoning", "summary": []any{}}
	idx := len(r.output)
	r.send("response.output_item.added", map[string]any{"output_index": idx, "item": r.openItem})
	r.send("response.reasoning_summary_part.added", map[string]any{"item_id": r.openItem["id"], "output_index": idx, "summary_index": 0,
		"part": map[string]any{"type": "summary_text", "text": ""}})
}

func (r *responsesEncoder) closeOpen() {
	idx := len(r.output)
	switch r.open {
	case "message":
		text := r.text.String()
		id := r.openItem["id"]
		part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
		r.send("response.output_text.done", map[string]any{"item_id": id, "output_index": idx, "content_index": 0, "text": text})
		r.send("response.content_part.done", map[string]any{"item_id": id, "output_index": idx, "content_index": 0, "part": part})
		item := map[string]any{"id": id, "type": "message", "role": "assistant", "status": "completed", "content": []any{part}}
		r.send("response.output_item.done", map[string]any{"output_index": idx, "item": item})
		r.output = append(r.output, item)
		r.text.Reset()
	case "reasoning":
		text := r.thinking.String()
		id := r.openItem["id"]
		summary := []any{}
		if text != "" {
			summary = append(summary, map[string]any{"type": "summary_text", "text": text})
			r.send("response.reasoning_summary_text.done", map[string]any{"item_id": id, "output_index": idx, "summary_index": 0, "text": text})
			r.send("response.reasoning_summary_part.done", map[string]any{"item_id": id, "output_index": idx, "summary_index": 0, "part": summary[0]})
		}
		item := map[string]any{"id": id, "type": "reasoning", "summary": summary}
		if r.seal && (r.sig.Len() > 0 || r.redacted != "") {
			item["encrypted_content"] = sealAnthropicThinking(text, r.sig.String(), r.redacted)
		}
		r.send("response.output_item.done", map[string]any{"output_index": idx, "item": item})
		r.output = append(r.output, item)
		r.thinking.Reset()
		r.sig.Reset()
		r.redacted = ""
	}
	r.open = ""
	r.openItem = nil
}

func (r *responsesEncoder) flushTools() {
	for _, t := range r.tools.take() {
		r.closeOpen()
		idx := len(r.output)
		args := t.arguments()
		info, known := r.names[t.name]
		var item map[string]any
		if known && info.custom {
			var input struct {
				Input string `json:"input"`
			}
			_ = json.Unmarshal([]byte(args), &input)
			item = map[string]any{"id": "ctc_" + randomID(), "type": "custom_tool_call", "status": "completed", "call_id": t.callID, "name": info.name, "input": input.Input}
			r.send("response.output_item.added", map[string]any{"output_index": idx, "item": item})
		} else {
			item = map[string]any{"id": "fc_" + randomID(), "type": "function_call", "status": "completed", "call_id": t.callID, "name": t.name, "arguments": args}
			if known {
				item["name"], item["namespace"] = info.name, info.namespace
			}
			added := map[string]any{}
			for k, v := range item {
				added[k] = v
			}
			added["arguments"], added["status"] = "", "in_progress"
			r.send("response.output_item.added", map[string]any{"output_index": idx, "item": added})
			r.send("response.function_call_arguments.delta", map[string]any{"item_id": item["id"], "output_index": idx, "delta": args})
			r.send("response.function_call_arguments.done", map[string]any{"item_id": item["id"], "output_index": idx, "arguments": args})
		}
		r.send("response.output_item.done", map[string]any{"output_index": idx, "item": item})
		r.output = append(r.output, item)
	}
}

func (r *responsesEncoder) handle(e Event) {
	switch e.Kind {
	case EvUsage:
		r.usage.add(e.Usage)
		return
	case EvToolArgs:
		r.tools.args(e)
		return
	}
	r.begin()
	switch e.Kind {
	case EvText:
		if e.Text == "" {
			return
		}
		r.flushTools()
		r.openMessage()
		r.text.WriteString(e.Text)
		r.send("response.output_text.delta", map[string]any{"item_id": r.openItem["id"], "output_index": len(r.output), "content_index": 0, "delta": e.Text})
	case EvThinking:
		if e.Text == "" {
			return
		}
		r.flushTools()
		r.openReasoning()
		r.thinking.WriteString(e.Text)
		r.send("response.reasoning_summary_text.delta", map[string]any{"item_id": r.openItem["id"], "output_index": len(r.output), "summary_index": 0, "delta": e.Text})
	case EvSignature:
		if e.Text == "" {
			return
		}
		// Claude 5 models sign a thinking block whose text they leave
		// out: the signature alone is what the next turn must send back.
		if r.open != "reasoning" {
			r.flushTools()
			r.openReasoning()
		}
		r.sig.WriteString(e.Text)
	case EvRedacted:
		r.flushTools()
		r.openReasoning()
		r.redacted = e.Text
		r.closeOpen()
	case EvToolStart:
		r.closeOpen()
		r.tools.start(e)
	case EvStop:
		r.stop = e.Stop
	}
}

func (r *responsesEncoder) usageJSON() map[string]any {
	u := r.usage
	input := u.Input + u.CacheRead + u.CacheWrite
	return map[string]any{
		"input_tokens":          input,
		"input_tokens_details":  map[string]int{"cached_tokens": u.CacheRead},
		"output_tokens":         u.Output,
		"output_tokens_details": map[string]int{"reasoning_tokens": u.Reasoning},
		"total_tokens":          input + u.Output,
	}
}

func (r *responsesEncoder) finish() {
	r.begin()
	r.flushTools()
	r.closeOpen()
	status := "completed"
	resp := r.response(status)
	if r.stop == StopLength {
		resp["status"] = "incomplete"
		resp["incomplete_details"] = map[string]string{"reason": "max_output_tokens"}
	}
	resp["usage"] = r.usageJSON()
	if !r.stream {
		body, _ := json.Marshal(resp)
		r.w.Header().Set("Content-Type", "application/json")
		r.w.WriteHeader(http.StatusOK)
		_, _ = r.w.Write(body)
		return
	}
	if resp["status"] == "incomplete" {
		r.send("response.incomplete", map[string]any{"response": resp})
		return
	}
	r.send("response.completed", map[string]any{"response": resp})
}

func (r *responsesEncoder) fail(e *Error) {
	if !r.started || !r.stream {
		status, body := OpenAIError(e)
		r.w.Header().Set("Content-Type", "application/json")
		r.w.WriteHeader(status)
		_, _ = r.w.Write(body)
		r.started = true
		return
	}
	_, body := OpenAIError(e)
	var wrapped struct {
		Error map[string]any `json:"error"`
	}
	_ = json.Unmarshal(body, &wrapped)
	resp := r.response("failed")
	resp["error"] = wrapped.Error
	r.send("response.failed", map[string]any{"response": resp})
}

func (r *responsesEncoder) keepAlive() {
	if r.started && r.stream {
		r.sse.comment()
	}
}

func (r *responsesEncoder) began() bool { return r.started }
