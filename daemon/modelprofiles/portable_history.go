package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// History portability / degradation vocabulary (daemon + wire).
const (
	// HistoryPortabilityStripOpaque means the Session has switched
	// HistoryDomain after opaque traffic. Responses routes strip provider-side
	// state (previous_response_id, reasoning, item_reference) on every later
	// request. Anthropic Messages routes drop only thinking blocks the current
	// upstream did not issue and forward every other byte unchanged.
	HistoryPortabilityStripOpaque = "strip_opaque_provider_state"
	// HistoryDegradationStripOpaque is the honest activation fact recorded when
	// a cross-domain activate relies on that strip boundary.
	HistoryDegradationStripOpaque = "strip_opaque_provider_state"
)

// portableHistoryProtocolsAllowed reports whether an opaque cross-domain
// activate may proceed. Responses carries provider-side state
// (previous_response_id / reasoning items / item_reference) that the router
// strips. Anthropic Messages history is client-owned; only thinking signed by
// another backend is dropped (dropForeignAnthropicThinking). No general
// translation.
func portableHistoryProtocolsAllowed(current, next RouteBinding) bool {
	if normalizeID(current.RouteProtocol) != normalizeID(next.RouteProtocol) {
		return false
	}
	if normalizeID(current.Protocol) != normalizeID(next.Protocol) {
		return false
	}
	switch normalizeID(current.RouteProtocol) {
	case RouteProtocolResponses, RouteProtocolAnthropicMessages:
		return true
	default:
		return false
	}
}

// normalizePortableHistoryBody strips provider-opaque history for a degraded
// Responses Session. Explicit policy:
//   - unknown item/content *types* are forwarded unchanged
//   - known portable types project to an allowlisted field set (drop extras; never
//     raw-forward provider-specific fields on tools/messages)
//   - strip known opaque state (previous_response_id, reasoning, item_reference)
//   - keep visible text and refusal
//   - malformed JSON/shapes fail closed (ErrRequestBodyMalformed)
func normalizePortableHistoryBody(routeProtocol string, body []byte) ([]byte, string, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return body, "", nil
	}
	switch normalizeID(routeProtocol) {
	case RouteProtocolResponses:
		out, err := normalizeResponsesPortableHistory(body)
		if err != nil {
			return nil, "", err
		}
		return out, HistoryDegradationStripOpaque, nil
	default:
		return nil, "", fmt.Errorf("%w: portable history unsupported for %s", ErrBindingHistoryState, routeProtocol)
	}
}

func normalizeResponsesPortableHistory(body []byte) ([]byte, error) {
	obj, err := decodeJSONObject(body)
	if err != nil {
		return nil, err
	}
	delete(obj, "previous_response_id")
	if raw, ok := obj["input"]; ok {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("%w: responses input must be an array: %v", ErrRequestBodyMalformed, err)
		}
		kept := make([]json.RawMessage, 0, len(items))
		for _, item := range items {
			keep, err := filterResponsesInputItem(item)
			if err != nil {
				return nil, err
			}
			if keep != nil {
				kept = append(kept, keep)
			}
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return nil, fmt.Errorf("%w: re-encode input: %v", ErrRequestBodyMalformed, err)
		}
		obj["input"] = json.RawMessage(encoded)
	}
	return encodeJSONObject(obj)
}

func filterResponsesInputItem(item json.RawMessage) (json.RawMessage, error) {
	var meta struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(item, &meta); err != nil {
		return nil, fmt.Errorf("%w: responses input item: %v", ErrRequestBodyMalformed, err)
	}
	switch normalizeID(meta.Type) {
	case "message":
		return filterResponsesMessageItem(item)
	case "function_call":
		return projectJSONObjectFields(item, []string{"type", "name", "call_id", "arguments", "id", "status"})
	case "function_call_output":
		return projectJSONObjectFields(item, []string{"type", "call_id", "output", "id", "status"})
	case "reasoning", "item_reference":
		return nil, nil
	case "":
		return nil, fmt.Errorf("%w: responses input item missing type", ErrRequestBodyMalformed)
	default:
		return item, nil
	}
}

func filterResponsesMessageItem(item json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return nil, fmt.Errorf("%w: message item: %v", ErrRequestBodyMalformed, err)
	}
	rawContent, ok := obj["content"]
	if !ok {
		return projectJSONObjectFieldsFromMap(obj, []string{"type", "role", "content", "id", "status"})
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(rawContent, &parts); err != nil {
		return nil, fmt.Errorf("%w: message content must be an array: %v", ErrRequestBodyMalformed, err)
	}
	kept := make([]json.RawMessage, 0, len(parts))
	for _, part := range parts {
		var meta struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(part, &meta); err != nil {
			return nil, fmt.Errorf("%w: message content part: %v", ErrRequestBodyMalformed, err)
		}
		switch normalizeID(meta.Type) {
		case "input_text", "output_text":
			proj, err := projectJSONObjectFields(part, []string{"type", "text"})
			if err != nil {
				return nil, err
			}
			kept = append(kept, proj)
		case "input_image", "output_image":
			proj, err := projectJSONObjectFields(part, []string{"type", "image_url", "detail", "file_id"})
			if err != nil {
				return nil, err
			}
			kept = append(kept, proj)
		case "refusal":
			// Visible refusal text must not silently disappear.
			proj, err := projectJSONObjectFields(part, []string{"type", "refusal"})
			if err != nil {
				return nil, err
			}
			kept = append(kept, proj)
		case "reasoning":
			continue
		case "":
			return nil, fmt.Errorf("%w: message content part missing type", ErrRequestBodyMalformed)
		default:
			if bytes.Contains(part, []byte(`encrypted_content`)) || strings.Contains(normalizeID(meta.Type), "encrypted") {
				continue
			}
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode message content: %v", ErrRequestBodyMalformed, err)
	}
	obj["content"] = json.RawMessage(encoded)
	return projectJSONObjectFieldsFromMap(obj, []string{"type", "role", "content", "id", "status"})
}

// projectJSONObjectFields keeps only allowlisted fields (drops extras to avoid
// leaking provider-specific state). Missing allowlisted fields are omitted.
func projectJSONObjectFields(raw json.RawMessage, allow []string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: object: %v", ErrRequestBodyMalformed, err)
	}
	return projectJSONObjectFieldsFromMap(obj, allow)
}

func projectJSONObjectFieldsFromMap(obj map[string]json.RawMessage, allow []string) (json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(allow))
	for _, k := range allow {
		if v, ok := obj[k]; ok {
			out[k] = v
		}
	}
	return encodeJSONObject(out)
}

func jsonRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func decodeJSONObject(body []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestBodyMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing junk after json object", ErrRequestBodyMalformed)
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: body must be a json object", ErrRequestBodyMalformed)
	}
	return obj, nil
}

func encodeJSONObject(obj map[string]json.RawMessage) ([]byte, error) {
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: re-encode: %v", ErrRequestBodyMalformed, err)
	}
	return out, nil
}
