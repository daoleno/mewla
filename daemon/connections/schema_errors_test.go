package connections

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// imageTool is a tool whose items array holds at most 8 entries, the shape that
// produced the bare "arguments do not match the tool schema" report.
func imageTool(t *testing.T) (*record, Tool) {
	t.Helper()
	tool := Tool{Name: "generate_image", Description: "Draw one image", InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {"type": "string"},
			"quality": {"type": "string", "enum": ["draft", "final"]},
			"items": {
				"type": "array",
				"maxItems": 8,
				"items": {
					"type": "object",
					"properties": {"url": {"type": "string"}},
					"required": ["url"],
					"additionalProperties": false
				}
			}
		},
		"required": ["prompt"],
		"additionalProperties": false
	}`)}
	r := &record{
		Account: Account{
			ID: "acc", Integration: "mcp", Name: "Onlora", Identity: "onlora",
			Enabled: true, Status: "connected", Tools: []Tool{tool},
		},
		Grants: map[string]string{tool.Name: fingerprint(tool)},
	}
	return r, tool
}

func imageItems(count int) []any {
	items := make([]any, count)
	for i := range items {
		items[i] = map[string]any{"url": "https://example.test/" + strconv.Itoa(i)}
	}
	return items
}

func TestInvokeSchemaErrorNamesTheFieldAndConstraint(t *testing.T) {
	r, _ := imageTool(t)
	args, err := json.Marshal(map[string]any{"prompt": "a red fox", "items": imageItems(9)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = checkInvoke(r, Request{Tool: "generate_image", Arguments: args})
	if err == nil {
		t.Fatal("nine items passed a maxItems 8 schema")
	}
	for _, want := range []string{"items:", "maxItems is 8", "9 items"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "items[0]") {
		t.Fatalf("error blamed an item instead of the array: %q", err)
	}
}

func TestInvokeSchemaErrorListsAtMostThreeMismatches(t *testing.T) {
	r, tool := imageTool(t)
	args, err := json.Marshal(map[string]any{
		"prompt":  7,             // wrong type
		"quality": "ultra",       // not one of the enum
		"items":   imageItems(9), // over maxItems
		"steps":   30,            // unexpected property
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = checkInvoke(r, Request{Tool: tool.Name, Arguments: args})
	if err == nil {
		t.Fatal("arguments that miss four constraints passed")
	}
	message := err.Error()
	for _, want := range []string{"prompt: has type integer, expected string", `quality: is "ultra"`, "items: has 9 items, maxItems is 8"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not name %q", message, want)
		}
	}
	if strings.Contains(message, `"steps"`) {
		t.Fatalf("error listed a fourth mismatch: %q", message)
	}
	if !strings.Contains(message, "and more") {
		t.Fatalf("error hid that it stopped at three: %q", message)
	}
	if strings.Count(message, ": ") > schemaMismatchLimit+1 {
		t.Fatalf("error listed too many mismatches: %q", message)
	}
}

func TestInvokeSchemaErrorFollowsLocalRefs(t *testing.T) {
	tool := Tool{Name: "generate_image", Description: "Draw one image", InputSchema: json.RawMessage(`{
		"type": "object",
		"$defs": {
			"item": {
				"type": "object",
				"properties": {"url": {"type": "string"}},
				"required": ["url", "role"]
			}
		},
		"properties": {"items": {"type": "array", "items": {"$ref": "#/$defs/item"}}},
		"required": ["items"]
	}`)}
	r := &record{
		Account: Account{
			ID: "acc", Integration: "mcp", Name: "Onlora", Identity: "onlora",
			Enabled: true, Status: "connected", Tools: []Tool{tool},
		},
		Grants: map[string]string{tool.Name: fingerprint(tool)},
	}
	args, err := json.Marshal(map[string]any{"items": []any{map[string]any{"url": "https://example.test/0"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = checkInvoke(r, Request{Tool: tool.Name, Arguments: args})
	if err == nil {
		t.Fatal("an item missing a required field passed")
	}
	if !strings.Contains(err.Error(), "items[0].role: is required") {
		t.Fatalf("error does not follow the $ref to the missing field: %q", err)
	}
}

func TestInvokeSchemaErrorPicksTheClosestAlternative(t *testing.T) {
	tool := Tool{Name: "generate_image", Description: "Draw one image", InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {"aspect": {"anyOf": [{"type": "string"}, {"type": "number"}]}},
		"required": ["aspect"]
	}`)}
	r := &record{
		Account: Account{
			ID: "acc", Integration: "mcp", Name: "Onlora", Identity: "onlora",
			Enabled: true, Status: "connected", Tools: []Tool{tool},
		},
		Grants: map[string]string{tool.Name: fingerprint(tool)},
	}
	args, err := json.Marshal(map[string]any{"aspect": map[string]any{"wide": true}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = checkInvoke(r, Request{Tool: tool.Name, Arguments: args})
	if err == nil {
		t.Fatal("an object passed an anyOf of string and number")
	}
	if !strings.Contains(err.Error(), "aspect: has type object, expected") {
		t.Fatalf("error does not name the field behind anyOf: %q", err)
	}
	if !strings.Contains(err.Error(), "none of 2 allowed shapes") {
		t.Fatalf("error hides that the field allows two shapes: %q", err)
	}
}

func TestInvokeSchemaAcceptsValidArguments(t *testing.T) {
	r, tool := imageTool(t)
	args, err := json.Marshal(map[string]any{"prompt": "a red fox", "quality": "final", "items": imageItems(8)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := checkInvoke(r, Request{Tool: tool.Name, Arguments: args}); err != nil {
		t.Fatalf("valid arguments were rejected: %v", err)
	}
}
