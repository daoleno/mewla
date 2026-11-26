package translate

import (
	"bytes"
	"encoding/json"
	"strings"
)

// UnsignedThinkingSignature is the signature Mewla gives a thinking block
// made from a Chat model's reasoning, which has none of its own.
const UnsignedThinkingSignature = "mewla.unsigned-thinking.v1"

// NativeHistory removes from a request what only translation put into the
// agent's history, before the request goes to an upstream that speaks the
// agent's own API (api). An agent keeps what a translated Provider answered
// and sends it again after the person picks another Provider:
//
//   - Codex: reasoning items whose encrypted_content is Mewla's seal of
//     Claude's thinking. A Responses upstream cannot decrypt them and refuses
//     every later turn.
//   - Claude Code: thinking blocks made from a Chat model's reasoning, which
//     carry UnsignedThinkingSignature. Anthropic refuses a thinking block it
//     did not sign. If that leaves a tool turn without its thinking while
//     thinking is on, thinking is turned off for the request, as Anthropic
//     refuses that too.
//
// A body with none of these, or one that cannot be read, is returned as it
// is.
func NativeHistory(api string, body []byte) []byte {
	switch api {
	case APIResponses:
		if !bytes.Contains(body, []byte(anthropicSealPrefix)) {
			return body
		}
		return withoutSealedReasoning(body)
	case APIAnthropic:
		if !bytes.Contains(body, []byte(UnsignedThinkingSignature)) {
			return body
		}
		return withoutUnsignedThinking(body)
	}
	return body
}

func withoutSealedReasoning(body []byte) []byte {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	var input []json.RawMessage
	if json.Unmarshal(obj["input"], &input) != nil {
		return body
	}
	kept := make([]json.RawMessage, 0, len(input))
	for _, raw := range input {
		var item struct {
			Type      string `json:"type"`
			Encrypted string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Type == "reasoning" && strings.HasPrefix(item.Encrypted, anthropicSealPrefix) {
			continue
		}
		kept = append(kept, raw)
	}
	if len(kept) == len(input) {
		return body
	}
	return withField(obj, "input", kept, body)
}

func withoutUnsignedThinking(body []byte) []byte {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	var msgs []map[string]json.RawMessage
	if json.Unmarshal(obj["messages"], &msgs) != nil {
		return body
	}
	changed := false
	keptMsgs := msgs[:0:0]
	for _, m := range msgs {
		var blocks []json.RawMessage
		if json.Unmarshal(m["content"], &blocks) != nil {
			keptMsgs = append(keptMsgs, m) // a string: nothing to look at
			continue
		}
		// Blocks stay as the agent wrote them; only their type and
		// signature are read.
		kept := blocks[:0:0]
		for _, raw := range blocks {
			var b struct {
				Type      string `json:"type"`
				Signature string `json:"signature"`
			}
			if json.Unmarshal(raw, &b) == nil && b.Type == "thinking" && b.Signature == UnsignedThinkingSignature {
				changed = true
				continue
			}
			kept = append(kept, raw)
		}
		if len(kept) != len(blocks) {
			if len(kept) == 0 {
				continue // a turn that was only that thinking
			}
			raw, err := json.Marshal(kept)
			if err != nil {
				return body
			}
			m["content"] = raw
		}
		keptMsgs = append(keptMsgs, m)
	}
	if !changed {
		return body
	}
	msgs = keptMsgs
	out, err := json.Marshal(msgs)
	if err != nil {
		return body
	}
	obj["messages"] = out
	if _, on := obj["thinking"]; on && !toolTurnKeepsThinking(msgs) {
		delete(obj, "thinking")
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return b
}

// toolTurnKeepsThinking reports whether the last assistant turn, when it
// called a tool, still opens with a thinking block: Anthropic refuses a
// request with thinking on when it does not.
func toolTurnKeepsThinking(msgs []map[string]json.RawMessage) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		var role string
		if json.Unmarshal(msgs[i]["role"], &role) != nil || role != RoleAssistant {
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msgs[i]["content"], &blocks) != nil {
			return true
		}
		calls := false
		for _, b := range blocks {
			if b.Type == "tool_use" {
				calls = true
			}
		}
		if !calls {
			return true
		}
		return blocks[0].Type == "thinking" || blocks[0].Type == "redacted_thinking"
	}
	return true
}

func withField(obj map[string]json.RawMessage, key string, value any, fallback []byte) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	obj[key] = raw
	out, err := json.Marshal(obj)
	if err != nil {
		return fallback
	}
	return out
}
