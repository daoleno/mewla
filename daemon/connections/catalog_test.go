package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCatalogEntriesAreValid(t *testing.T) {
	for _, e := range integrations {
		if err := e.validate(); err != nil {
			t.Errorf("%s: %v", e.ID, err)
		}
		if lookup(e.ID) != e {
			t.Errorf("%s is not indexed", e.ID)
		}
	}
}

func TestCatalogRejectsIncompleteEntries(t *testing.T) {
	base := func() entry {
		return entry{
			Integration: Integration{ID: "x", Name: "X", SetupURL: "https://x.example", Description: "x"},
			Driver:      "rest", Endpoint: "https://api.x.example", IdentityPath: "/me", Tools: githubTools,
		}
	}
	good := base()
	if err := good.validate(); err != nil {
		t.Fatalf("a complete entry was refused: %v", err)
	}
	for name, mutate := range map[string]func(*entry){
		"no name":             func(e *entry) { e.Name = "" },
		"unknown driver":      func(e *entry) { e.Driver = "nope" },
		"plain http endpoint": func(e *entry) { e.Endpoint = "http://api.x.example" },
		"no tools":            func(e *entry) { e.Tools = nil },
		"no identity path":    func(e *entry) { e.IdentityPath = "" },
		"custom with fixed endpoint": func(e *entry) {
			e.Custom = true
		},
		"credential without endpoint": func(e *entry) { e.Endpoint, e.NeedsCredential = "", true },
		"unknown codec": func(e *entry) {
			e.OAuth = &oauthSpec{Source: clientRegistered, Codec: "nope", Public: nil}
		},
		"registered client without mcp": func(e *entry) { e.OAuth = &oauthSpec{Source: clientRegistered} },
		"publisher client without id":   func(e *entry) { e.OAuth = &oauthSpec{Source: clientPublisher} },
		"identity tool is not a read": func(e *entry) {
			e.MCP = &mcpSpec{Endpoint: "https://mcp.x.example/mcp", Reads: []string{"a"}, Writes: []string{"b"}, Identity: "b"}
		},
		"tool both read and write": func(e *entry) {
			e.MCP = &mcpSpec{Endpoint: "https://mcp.x.example/mcp", Reads: []string{"a"}, Writes: []string{"a"}}
		},
	} {
		e := base()
		mutate(&e)
		if e.validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Every reviewed MCP tool keeps its class. Linear's server saves through
// save_* tools, which were once missing and so could not be allowed.
func TestOfficialToolClassesAreReviewed(t *testing.T) {
	linear := lookup("linear").MCP
	for _, name := range []string{"save_issue", "save_comment", "save_project", "save_document", "save_milestone", "save_status_update"} {
		if linear.group(name) != "write" {
			t.Errorf("%s is %q, not write", name, linear.group(name))
		}
	}
	if linear.group("get_issue") != "read" || linear.group("delete_everything") != "" {
		t.Error("Linear read or unknown tools are misclassified")
	}
	for _, e := range integrations {
		if e.MCP == nil {
			continue
		}
		for _, name := range e.MCP.Reads {
			if contains(e.MCP.Writes, name) {
				t.Errorf("%s lists %s as read and write", e.ID, name)
			}
		}
	}
}

// A new REST integration is one catalog entry: the generic code adds it,
// offers its reviewed tools, applies its headers and defaults, and refuses a
// call to anything it has not reviewed.
func TestNewIntegrationNeedsOnlyACatalogEntry(t *testing.T) {
	fake := &entry{
		Integration: Integration{ID: "fake", Name: "Fake", Available: true, SetupURL: "https://fake.example", Description: "A fake service"},
		Driver:      "rest", Endpoint: "https://api.fake.example/v1", NeedsCredential: true, IdentityPath: "/whoami",
		Headers:       map[string]string{"X-Fake-Version": "7"},
		QueryDefaults: []queryDefault{{Tool: "list_things", Param: "limit", Value: "5"}},
		Tools: func() []Tool {
			return []Tool{
				builtin("get_me", "Read the account", "GET", "/whoami", true, nil, nil, nil),
				builtin("list_things", "List things", "GET", "/things", true, nil, object(map[string]any{"limit": pageSize()}), nil),
				builtin("make_thing", "Make a thing", "POST", "/things", false, nil, nil, object(map[string]any{"name": str()}, "name")),
			}
		},
	}
	if err := fake.validate(); err != nil {
		t.Fatal(err)
	}
	integrations = append(integrations, fake)
	byID["fake"] = fake
	hostRules["api.fake.example"] = fake
	t.Cleanup(func() {
		integrations = integrations[:len(integrations)-1]
		delete(byID, "fake")
		delete(hostRules, "api.fake.example")
	})

	var seen []string
	m := testManager(t, func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.Method+" "+req.URL.String()+" "+req.Header.Get("X-Fake-Version"))
		if req.URL.Path == "/v1/whoami" {
			return reply(200, `{"id":"f1","name":"Fay"}`), nil
		}
		return reply(200, `[]`), nil
	})
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "fake", Name: "F"}}); err == nil || !strings.Contains(err.Error(), "authorized account") {
		t.Fatalf("a credential was not required: %v", err)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "fake", Name: "F", Credential: "fake-secret-key", TrustedNetworks: []string{"10.0.0.0/8"}}}); err == nil {
		t.Fatal("a built-in service accepted internal network trust")
	}
	account := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "fake", Name: "F", Credential: "fake-secret-key"}}).Account
	if account.Identity != "Fay" || account.Endpoint != "https://api.fake.example/v1" {
		t.Fatalf("account %+v", account)
	}
	groups := map[string]string{}
	for _, tool := range account.Tools {
		groups[tool.Name] = tool.Group
		if tool.Allowed != (tool.Group == "read") {
			t.Errorf("%s allowed=%v in group %q", tool.Name, tool.Allowed, tool.Group)
		}
	}
	if groups["get_me"] != "read" || groups["list_things"] != "read" || groups["make_thing"] != "write" {
		t.Fatalf("groups %v", groups)
	}
	seen = nil
	mustHandle(t, m, Request{Action: "invoke", ID: account.ID, Tool: "list_things"})
	if len(seen) != 1 || seen[0] != "GET https://api.fake.example/v1/things?limit=5 7" {
		t.Fatalf("request %v", seen)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "make_thing", Arguments: json.RawMessage(`{"body":{"name":"n"}}`)}); err == nil {
		t.Fatal("a change ran without being allowed")
	}
	found := false
	for _, item := range m.catalog() {
		found = found || item.ID == "fake" && item.Available
	}
	if !found {
		t.Fatal("the new entry is not in the catalog")
	}
}
