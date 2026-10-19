package connections

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// schemaTestRecord grants one tool so a call reaches schema validation. The
// items array holds at most 8 entries, the shape that produced the bare
// "arguments do not match the tool schema" report.
func schemaTestRecord() *record {
	tool := Tool{Name: "generate_image", Description: "Draw one image", InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {"type": "string"},
			"items": {
				"type": "array",
				"maxItems": 8,
				"items": {
					"type": "object",
					"properties": {"url": {"type": "string"}, "prompt": {"type": "string"}},
					"required": ["url"]
				}
			}
		},
		"required": ["prompt"]
	}`)}
	return &record{
		Account: Account{
			ID: "acc", Integration: "mcp", Name: "Onlora", Identity: "onlora",
			Enabled: true, Status: "connected", Tools: []Tool{tool},
		},
		Grants: map[string]string{tool.Name: fingerprint(tool)},
	}
}

func TestInvokeSchemaErrorNamesFieldAndConstraint(t *testing.T) {
	fullItems := make([]any, 9)
	for i := range fullItems {
		fullItems[i] = map[string]any{"url": "https://example.test/" + strconv.Itoa(i)}
	}
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing required field",
			args: map[string]any{},
			want: `arguments do not match the tool schema: required: missing properties: ["prompt"]`,
		},
		{
			name: "wrong type names the nested path",
			args: map[string]any{
				"prompt": "a red fox",
				"items":  []any{map[string]any{"url": "https://example.test/0", "prompt": 1}},
			},
			want: `items[].prompt: type: 1 has type "integer", want "string"`,
		},
		{
			name: "maxItems names the array and the limit",
			args: map[string]any{"prompt": "a red fox", "items": fullItems},
			want: `items: maxItems: array length 9 is greater than 8`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args, err := json.Marshal(c.args)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = checkInvoke(schemaTestRecord(), Request{Tool: "generate_image", Arguments: args})
			if err == nil {
				t.Fatalf("arguments %s passed the tool schema", args)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
			if strings.Contains(err.Error(), "validating ") {
				t.Fatalf("error kept the library's nested frames: %q", err)
			}
			if len([]rune(err.Error())) > maxSchemaMismatchDetail {
				t.Fatalf("error is %d characters, over the %d limit: %q", len([]rune(err.Error())), maxSchemaMismatchDetail, err)
			}
		})
	}
}

func TestInvokeSchemaAcceptsValidArguments(t *testing.T) {
	args, err := json.Marshal(map[string]any{
		"prompt": "a red fox",
		"items":  []any{map[string]any{"url": "https://example.test/0", "prompt": "wide"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := checkInvoke(schemaTestRecord(), Request{Tool: "generate_image", Arguments: args}); err != nil {
		t.Fatalf("valid arguments were rejected: %v", err)
	}
}
