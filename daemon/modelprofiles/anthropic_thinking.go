package modelprofiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Anthropic thinking blocks carry a signature that only the issuing backend
// verifies. After a Session switches provider, the Claude CLI keeps resending
// the old backend's signed thinking, which the new upstream rejects. The router
// therefore learns which signatures each upstream issued (from its responses)
// and, on switched routes only, drops thinking blocks the current upstream did
// not issue. Every other byte of the request is forwarded unchanged.

const (
	maxIssuedThinkingPerDomain = 8192
	// maxRecordedResponseBytes bounds buffering of a non-streaming response
	// while looking for issued signatures; larger bodies are forwarded but not
	// learned (their thinking is dropped once, which upstreams accept).
	maxRecordedResponseBytes = 16 << 20
	// anthropicThinkingOmittedText keeps an assistant turn non-empty when all
	// of its content was foreign thinking.
	anthropicThinkingOmittedText = "[provider thinking omitted]"
)

// issuedThinkingLedger remembers, per route and history domain, the thinking
// signatures that domain's upstream issued. Memory only: after a daemon restart
// a switched route drops its thinking once and relearns from new responses.
type issuedThinkingLedger struct {
	mu      sync.Mutex
	domains map[string]*issuedThinkingSet
}

type issuedThinkingSet struct {
	keys  map[string]struct{}
	order []string
}

func newIssuedThinkingLedger() *issuedThinkingLedger {
	return &issuedThinkingLedger{domains: map[string]*issuedThinkingSet{}}
}

func issuedThinkingScope(routeID, domain string) string {
	return routeID + "\x00" + domain
}

func issuedThinkingKey(signature string) string {
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:16])
}

func (l *issuedThinkingLedger) record(routeID, domain, signature string) {
	if l == nil || signature == "" {
		return
	}
	scope := issuedThinkingScope(routeID, domain)
	key := issuedThinkingKey(signature)
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.domains[scope]
	if set == nil {
		set = &issuedThinkingSet{keys: map[string]struct{}{}}
		l.domains[scope] = set
	}
	if _, ok := set.keys[key]; ok {
		return
	}
	set.keys[key] = struct{}{}
	set.order = append(set.order, key)
	if len(set.order) > maxIssuedThinkingPerDomain {
		evict := set.order[0]
		set.order = set.order[1:]
		delete(set.keys, evict)
	}
}

func (l *issuedThinkingLedger) issued(routeID, domain, signature string) bool {
	if l == nil || signature == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.domains[issuedThinkingScope(routeID, domain)]
	if set == nil {
		return false
	}
	_, ok := set.keys[issuedThinkingKey(signature)]
	return ok
}

// anthropicThinkingRecorder observes an upstream Messages response (SSE or
// JSON) as it is forwarded and reports every issued thinking signature.
type anthropicThinkingRecorder struct {
	sse     bool
	onIssue func(string)
	line    []byte
	body    bytes.Buffer
	over    bool
	// SSE signature deltas accumulate per content block index.
	pending map[int]*strings.Builder
}

func newAnthropicThinkingRecorder(contentType string, onIssue func(string)) *anthropicThinkingRecorder {
	return &anthropicThinkingRecorder{
		sse:     strings.Contains(strings.ToLower(contentType), "text/event-stream"),
		onIssue: onIssue,
		pending: map[int]*strings.Builder{},
	}
}

func (r *anthropicThinkingRecorder) Write(p []byte) (int, error) {
	if !r.sse {
		if !r.over {
			if r.body.Len()+len(p) > maxRecordedResponseBytes {
				r.over = true
				r.body.Reset()
			} else {
				r.body.Write(p)
			}
		}
		return len(p), nil
	}
	rest := p
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			r.line = append(r.line, rest...)
			break
		}
		r.line = append(r.line, rest[:i]...)
		r.sseLine(r.line)
		r.line = r.line[:0]
		rest = rest[i+1:]
	}
	return len(p), nil
}

func (r *anthropicThinkingRecorder) sseLine(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || payload[0] != '{' {
		return
	}
	var ev struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock *struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
			Data      string `json:"data"`
		} `json:"content_block"`
		Delta *struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "content_block_start":
		if ev.ContentBlock == nil {
			return
		}
		switch ev.ContentBlock.Type {
		case "redacted_thinking":
			r.onIssue(ev.ContentBlock.Data)
		case "thinking":
			b := &strings.Builder{}
			b.WriteString(ev.ContentBlock.Signature)
			r.pending[ev.Index] = b
		}
	case "content_block_delta":
		if ev.Delta != nil && ev.Delta.Type == "signature_delta" {
			if b := r.pending[ev.Index]; b != nil {
				b.WriteString(ev.Delta.Signature)
			}
		}
	case "content_block_stop":
		if b := r.pending[ev.Index]; b != nil {
			r.onIssue(b.String())
			delete(r.pending, ev.Index)
		}
	}
}

// Finish parses a buffered JSON response once the body has been forwarded.
func (r *anthropicThinkingRecorder) Finish() {
	if r.sse {
		if len(r.line) > 0 {
			r.sseLine(r.line)
			r.line = nil
		}
		return
	}
	if r.over || r.body.Len() == 0 {
		return
	}
	var msg struct {
		Content []struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
			Data      string `json:"data"`
		} `json:"content"`
	}
	if err := json.Unmarshal(r.body.Bytes(), &msg); err != nil {
		return
	}
	for _, part := range msg.Content {
		switch part.Type {
		case "thinking":
			r.onIssue(part.Signature)
		case "redacted_thinking":
			r.onIssue(part.Data)
		}
	}
}

// dropForeignAnthropicThinking removes thinking / redacted_thinking blocks
// whose signature the current upstream did not issue. Messages without such
// blocks keep their exact bytes, and so does everything outside "messages".
// The returned flag reports whether anything was removed.
func dropForeignAnthropicThinking(body []byte, issued func(signature string) bool) ([]byte, bool, error) {
	fields, err := jsonObjectSpans(body)
	if err != nil {
		return nil, false, err
	}
	msgSpan, ok := fields["messages"]
	if !ok {
		return body, false, nil
	}
	msgsRaw := body[msgSpan.start:msgSpan.end]
	if len(msgsRaw) == 0 || msgsRaw[0] != '[' {
		return nil, false, fmt.Errorf("%w: anthropic messages must be an array", ErrRequestBodyMalformed)
	}
	elems, err := jsonArraySpans(msgsRaw)
	if err != nil {
		return nil, false, err
	}
	var out bytes.Buffer
	changed := false
	last := 0
	for _, el := range elems {
		msg := msgsRaw[el.start:el.end]
		rewritten, msgChanged, err := dropForeignThinkingInMessage(msg, issued)
		if err != nil {
			return nil, false, err
		}
		if !msgChanged {
			continue
		}
		changed = true
		out.Write(msgsRaw[last:el.start])
		out.Write(rewritten)
		last = el.end
	}
	if !changed {
		return body, false, nil
	}
	out.Write(msgsRaw[last:])
	result := make([]byte, 0, len(body))
	result = append(result, body[:msgSpan.start]...)
	result = append(result, out.Bytes()...)
	result = append(result, body[msgSpan.end:]...)
	return result, true, nil
}

func dropForeignThinkingInMessage(msg []byte, issued func(string) bool) ([]byte, bool, error) {
	if len(msg) == 0 || msg[0] != '{' {
		return nil, false, fmt.Errorf("%w: anthropic message must be an object", ErrRequestBodyMalformed)
	}
	fields, err := jsonObjectSpans(msg)
	if err != nil {
		return nil, false, err
	}
	contentSpan, ok := fields["content"]
	if !ok {
		return msg, false, nil
	}
	content := msg[contentSpan.start:contentSpan.end]
	if len(content) == 0 || content[0] != '[' {
		return msg, false, nil // string content carries no thinking
	}
	parts, err := jsonArraySpans(content)
	if err != nil {
		return nil, false, err
	}
	kept := make([][]byte, 0, len(parts))
	dropped := false
	for _, p := range parts {
		part := content[p.start:p.end]
		var meta struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
			Data      string `json:"data"`
		}
		if err := json.Unmarshal(part, &meta); err != nil {
			return nil, false, fmt.Errorf("%w: anthropic content part: %v", ErrRequestBodyMalformed, err)
		}
		signature := ""
		switch meta.Type {
		case "thinking":
			signature = meta.Signature
		case "redacted_thinking":
			signature = meta.Data
		default:
			kept = append(kept, part)
			continue
		}
		if issued(signature) {
			kept = append(kept, part)
			continue
		}
		dropped = true
	}
	if !dropped {
		return msg, false, nil
	}
	var newContent []byte
	if len(kept) == 0 {
		placeholder, _ := json.Marshal([]map[string]string{{"type": "text", "text": anthropicThinkingOmittedText}})
		newContent = placeholder
	} else {
		newContent = append([]byte{'['}, bytes.Join(kept, []byte{','})...)
		newContent = append(newContent, ']')
	}
	out := make([]byte, 0, len(msg))
	out = append(out, msg[:contentSpan.start]...)
	out = append(out, newContent...)
	out = append(out, msg[contentSpan.end:]...)
	return out, true, nil
}

type jsonSpan struct{ start, end int }

// jsonObjectSpans returns the byte span of each top-level member value.
func jsonObjectSpans(raw []byte) (map[string]jsonSpan, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("%w: body must be a json object", ErrRequestBodyMalformed)
	}
	spans := map[string]jsonSpan{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
		}
		key, _ := keyTok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
		}
		end := int(dec.InputOffset())
		spans[key] = jsonSpan{start: end - len(val), end: end}
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing data after json object", ErrRequestBodyMalformed)
	}
	return spans, nil
}

// jsonArraySpans returns the byte span of each element of a JSON array.
func jsonArraySpans(raw []byte) ([]jsonSpan, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, fmt.Errorf("%w: expected json array", ErrRequestBodyMalformed)
	}
	var spans []jsonSpan
	for dec.More() {
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
		}
		end := int(dec.InputOffset())
		spans = append(spans, jsonSpan{start: end - len(val), end: end})
	}
	return spans, nil
}

// recordingReader tees a response body into an anthropicThinkingRecorder.
func recordingReader(src io.Reader, rec *anthropicThinkingRecorder) io.Reader {
	return io.TeeReader(src, rec)
}
