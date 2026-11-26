package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// namespaceSep joins a Responses namespace and a tool name into the one name
// other APIs are offered: few models know namespaces, and tool names may not
// hold dots.
const namespaceSep = "__"

type responsesRequest struct {
	Model        string          `json:"model"`
	Instructions string          `json:"instructions,omitempty"`
	Input        json.RawMessage `json:"input"`
	Tools        []responsesTool `json:"tools,omitempty"`
	ToolChoice   json.RawMessage `json:"tool_choice,omitempty"`
	Parallel     *bool           `json:"parallel_tool_calls,omitempty"`
	Reasoning    *struct {
		Effort string `json:"effort,omitempty"`
	} `json:"reasoning,omitempty"`
	MaxOutput   int      `json:"max_output_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
	CacheKey    string   `json:"prompt_cache_key,omitempty"`
	User        string   `json:"user,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Tools       []responsesTool `json:"tools,omitempty"`
}

type responsesItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Namespace string          `json:"namespace,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Input     string          `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Summary   []struct {
		Text string `json:"text"`
	} `json:"summary,omitempty"`
	Encrypted string `json:"encrypted_content,omitempty"`
}

type responsesContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	FileData string `json:"file_data,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// ReadResponses reads an OpenAI Responses request.
func ReadResponses(body []byte) (*Request, error) {
	var in responsesRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("responses request: %w", err)
	}
	req := &Request{
		Model: in.Model, MaxTokens: in.MaxOutput, Temperature: in.Temperature, TopP: in.TopP,
		Stream: in.Stream, Parallel: in.Parallel, CacheKey: in.CacheKey, User: in.User,
	}
	if in.Reasoning != nil {
		req.Effort = in.Reasoning.Effort
		req.Thinking = in.Reasoning.Effort != "none"
	}
	if strings.TrimSpace(in.Instructions) != "" {
		req.System = append(req.System, Part{Kind: PartText, Text: in.Instructions})
	}
	items, err := responsesItems(in.Input)
	if err != nil {
		return nil, err
	}
	// Developer and system messages before the conversation are the
	// system prompt; one that comes later is told to the model as the user.
	started := false
	for _, it := range items {
		switch it.Type {
		case "message", "":
			parts, err := responsesParts(it.Content)
			if err != nil {
				return nil, err
			}
			role := it.Role
			if role == "developer" || role == "system" {
				if !started {
					req.System = append(req.System, parts...)
					continue
				}
				role = RoleUser
			}
			started = true
			if role != RoleAssistant {
				role = RoleUser
			}
			req.add(role, parts...)
		case "function_call":
			started = true
			name := it.Name
			if it.Namespace != "" {
				name = it.Namespace + namespaceSep + it.Name
			}
			args := it.Arguments
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			req.add(RoleAssistant, Part{Kind: PartToolCall, CallID: it.CallID, Name: name, Args: args})
		case "custom_tool_call":
			started = true
			args, _ := json.Marshal(map[string]string{"input": it.Input})
			req.add(RoleAssistant, Part{Kind: PartToolCall, CallID: it.CallID, Name: it.Name, Args: string(args), Custom: true})
		case "function_call_output", "custom_tool_call_output":
			started = true
			req.add(RoleUser, Part{Kind: PartToolResult, CallID: it.CallID, Result: responsesOutput(it.Output)})
		case "reasoning":
			started = true
			p := Part{Kind: PartThinking, Sealed: it.Encrypted}
			for _, s := range it.Summary {
				if p.Text != "" {
					p.Text += "\n\n"
				}
				p.Text += s.Text
			}
			if sig, thinking, ok := unsealAnthropicThinking(it.Encrypted); ok {
				p = Part{Kind: PartThinking, Text: thinking.Text, Signature: sig, Redacted: thinking.Redacted}
			}
			req.add(RoleAssistant, p)
		}
		// Built-in tool records (web_search_call, local_shell_call,
		// item_reference, …) belong to OpenAI's own tools; other APIs ran
		// none of them.
	}
	for _, t := range in.Tools {
		switch t.Type {
		case "function":
			req.Tools = append(req.Tools, Tool{Name: t.Name, Description: t.Description, Schema: t.Parameters})
		case "custom":
			req.Tools = append(req.Tools, Tool{Name: t.Name, Description: t.Description, Custom: true, Schema: customToolSchema})
		case "namespace":
			for _, nt := range t.Tools {
				if nt.Type == "function" {
					desc := nt.Description
					if t.Description != "" && desc == "" {
						desc = t.Description
					}
					req.Tools = append(req.Tools, Tool{Name: t.Name + namespaceSep + nt.Name, Description: desc, Schema: nt.Parameters})
				}
			}
		}
	}
	req.Choice = responsesChoice(in.ToolChoice)
	return req, nil
}

// customToolSchema is how a freeform tool is offered as a function.
var customToolSchema = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string","description":"The tool's input, as plain text."}},"required":["input"]}`)

// add appends parts to the conversation, joining a run of the same role.
func (r *Request) add(role string, parts ...Part) {
	if len(parts) == 0 {
		return
	}
	if n := len(r.Messages); n > 0 && r.Messages[n-1].Role == role {
		r.Messages[n-1].Parts = append(r.Messages[n-1].Parts, parts...)
		return
	}
	r.Messages = append(r.Messages, Message{Role: role, Parts: parts})
}

func responsesItems(raw json.RawMessage) ([]responsesItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		text, _ := json.Marshal(s)
		return []responsesItem{{Type: "message", Role: RoleUser, Content: text}}, nil
	}
	var items []responsesItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("responses input: %w", err)
	}
	return items, nil
}

func responsesParts(raw json.RawMessage) ([]Part, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []Part{{Kind: PartText, Text: s}}, nil
	}
	var content []responsesContent
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, fmt.Errorf("responses content: %w", err)
	}
	var parts []Part
	for _, c := range content {
		switch c.Type {
		case "input_text", "output_text", "text", "refusal":
			parts = append(parts, Part{Kind: PartText, Text: c.Text})
		case "input_image":
			parts = append(parts, imagePart(c.ImageURL))
		case "input_file":
			p := Part{Kind: PartFile, Filename: c.Filename, URL: c.FileURL}
			if c.FileData != "" {
				p.MediaType, p.Data = splitDataURL(c.FileData, "application/pdf")
			}
			parts = append(parts, p)
		}
	}
	return parts, nil
}

// responsesOutput reads a tool output: a string or a list of content.
func responsesOutput(raw json.RawMessage) []Part {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return []Part{{Kind: PartText, Text: s}}
	}
	parts, err := responsesParts(raw)
	if err != nil {
		return []Part{{Kind: PartText, Text: string(raw)}}
	}
	return parts
}

func responsesChoice(raw json.RawMessage) ToolChoice {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ToolChoice{}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		switch s {
		case "none":
			return ToolChoice{Mode: ChoiceNone}
		case "required":
			return ToolChoice{Mode: ChoiceAny}
		}
		return ToolChoice{}
	}
	var obj struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Name != "" {
		return ToolChoice{Mode: ChoiceTool, Name: obj.Name}
	}
	return ToolChoice{}
}

// imagePart reads an image given as a data: URL or a plain URL.
func imagePart(url string) Part {
	if strings.HasPrefix(url, "data:") {
		mt, data := splitDataURL(url, "image/png")
		return Part{Kind: PartImage, MediaType: mt, Data: data}
	}
	return Part{Kind: PartImage, URL: url}
}

// splitDataURL splits data:<type>;base64,<data>.
func splitDataURL(s, fallback string) (mediaType, data string) {
	if !strings.HasPrefix(s, "data:") {
		return fallback, s
	}
	head, data, ok := strings.Cut(strings.TrimPrefix(s, "data:"), ",")
	if !ok {
		return fallback, ""
	}
	mediaType = strings.TrimSuffix(head, ";base64")
	if mediaType == "" {
		mediaType = fallback
	}
	return mediaType, data
}

func dataURL(mediaType, data string) string {
	return "data:" + mediaType + ";base64," + data
}
