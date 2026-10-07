package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func testManager(t *testing.T, rt roundTripFunc) *Manager {
	t.Helper()
	m, err := newManager(filepath.Join(t.TempDir(), "catalog.json"), modelprofiles.NewMemoryCredentialStore(), &http.Client{Transport: rt})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func mustHandle(t *testing.T, m *Manager, q Request) Response {
	t.Helper()
	r, err := m.Handle(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func addGitHub(t *testing.T, m *Manager, name, secret string) *Account {
	t.Helper()
	return mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "github", Name: name, Credential: secret}}).Account
}

func TestAccountIsolationPolicyPersistenceAndRevocation(t *testing.T) {
	var calls atomic.Int32
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		return reply(200, `{"id":1,"login":"`+token+`-identity"}`), nil
	})
	// Discovery must reject a provider reflecting the credential into its identity.
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "github", Name: "bad", Credential: "echo-secret"}}); err == nil {
		t.Fatal("credential reflected in identity")
	}
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "secret-a" {
			return reply(200, `{"id":1,"login":"alice"}`), nil
		}
		return reply(200, `{"id":2,"login":"bob"}`), nil
	})
	a := addGitHub(t, m, "personal", "secret-a")
	b := addGitHub(t, m, "work", "secret-b")
	if a.Identity != "alice" || b.Identity != "bob" || a.ID == b.ID {
		t.Fatalf("bad identities: %s %s", a.Identity, b.Identity)
	}
	q := Request{Action: "invoke", ID: a.ID, Tool: "get_me", Arguments: json.RawMessage(`{}`)}
	result := mustHandle(t, m, q)
	if !bytes.Contains(result.Result, []byte("alice")) {
		t.Fatal(string(result.Result))
	}
	count := calls.Load()
	for _, request := range []Request{
		{Action: "invoke", ID: b.ID, Tool: "get_me", Arguments: json.RawMessage(`{"credential":"secret-a"}`)},
		{Action: "invoke", ID: a.ID, Tool: "create_issue", Arguments: json.RawMessage(`{"path":{"owner":"o","repo":"r"},"body":{"title":"test"}}`)},
	} {
		if _, err := m.Handle(context.Background(), request); err == nil {
			t.Fatal("expected denial")
		}
	}
	if calls.Load() != count {
		t.Fatal("invalid or unauthorized call reached service")
	}
	mustHandle(t, m, Request{Action: "disable", ID: a.ID})
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("disabled invocation")
	}
	mustHandle(t, m, Request{Action: "enable", ID: a.ID})
	reopened, err := newManager(m.path, m.vault, m.http)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(mustHandle(t, reopened, q).Result, []byte("alice")) {
		t.Fatal("restart lost account")
	}
	mustHandle(t, reopened, Request{Action: "disconnect", ID: a.ID})
	if _, err := reopened.Handle(context.Background(), q); err == nil {
		t.Fatal("revoked invocation")
	}
	if _, ok, _ := m.vault.Get("integration:" + a.ID); ok {
		t.Fatal("credential retained")
	}
	q.ID = b.ID
	if !bytes.Contains(mustHandle(t, reopened, q).Result, []byte("bob")) {
		t.Fatal("revocation crossed account")
	}
	raw, _ := os.ReadFile(m.path)
	if bytes.Contains(raw, []byte("secret-")) {
		t.Fatal("secret in metadata")
	}
	info, _ := os.Stat(m.path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("catalog not private")
	}
}

func TestInvocationRedactionErrorsAndTimeout(t *testing.T) {
	m := testManager(t, func(r *http.Request) (*http.Response, error) { return reply(200, `{"id":1,"login":"alice"}`), nil })
	a := addGitHub(t, m, "personal", "credential-secret")
	q := Request{Action: "invoke", ID: a.ID, Tool: "get_me"}
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return reply(200, `{"echo":"credential-secret"}`), nil })
	if bytes.Contains(mustHandle(t, m, q).Result, []byte("credential-secret")) {
		t.Fatal("secret leaked in result")
	}
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return reply(401, `credential-secret`), nil })
	if _, err := m.Handle(context.Background(), q); !errors.Is(err, errAuth) {
		t.Fatal(err)
	}
	a = mustHandle(t, m, Request{Action: "get", ID: a.ID}).Account
	if a.Status != "authorization_required" || a.History[0].Status != "error" {
		t.Fatal(a.Status)
	}
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := m.Handle(ctx, q); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatal(err)
	}
}

func TestAccountNamesAndDocumentSecrets(t *testing.T) {
	var calls int
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return reply(200, `{"id":1,"login":"alice"}`), nil
	})
	addGitHub(t, m, "personal", "test-secret")
	requests := []Input{
		{Integration: "github", Name: " personal ", Credential: "another-secret"},
		{Integration: "github", Name: "second", Credential: "test-secret", Spec: json.RawMessage(`{"unused":"test-secret"}`)},
		{Integration: "openapi", Name: "custom", Endpoint: "https://example.com", Credential: "test-secret", Spec: json.RawMessage(strings.Replace(fixtureSpec, `"title":"Fixture"`, `"title":"Fixture","description":"Bearer test-secret"`, 1))},
	}
	for _, in := range requests {
		if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &in}); err == nil {
			t.Fatal("accepted ambiguous name or credential in persisted document")
		}
	}
	if calls != 1 || len(m.records) != 1 {
		t.Fatal("rejected configuration changed accounts or reached provider")
	}
	raw, err := os.ReadFile(m.path)
	if err != nil || bytes.Contains(raw, []byte("test-secret")) {
		t.Fatal("credential in catalog")
	}
}

const fixtureSpec = `{"openapi":"3.0.3","info":{"title":"Fixture","version":"1"},"paths":{"/items/{id}":{"get":{"operationId":"read_item","parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"ok"}}}}}}`

func TestOpenAPIDiscoveryGrantAndDispatch(t *testing.T) {
	var calls int
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://example.com/api/items/42" || r.Header.Get("Authorization") != "Bearer custom-secret" {
			t.Fatal("wrong account dispatch")
		}
		return reply(200, `{"id":42}`), nil
	})
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "openapi", Name: "custom", Endpoint: "https://example.com/api", Credential: "custom-secret", Spec: json.RawMessage(fixtureSpec)}}).Account
	if a.Status != "configured" || a.VerifiedAt != nil || calls != 0 {
		t.Fatal("schema falsely verified account")
	}
	q := Request{Action: "invoke", ID: a.ID, Tool: "read_item", Arguments: json.RawMessage(`{"path":{"id":"42"}}`)}
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("GET auto-authorized")
	}
	mustHandle(t, m, Request{Action: "policy", ID: a.ID, Tool: q.Tool, Allowed: true})
	mustHandle(t, m, q)
	if calls != 1 {
		t.Fatal(calls)
	}
	refreshed := mustHandle(t, m, Request{Action: "refresh", ID: a.ID}).Account
	if refreshed.Status != "connected" || refreshed.Identity != "Account identity unavailable" || calls != 1 {
		t.Fatal("local schema refresh changed last observed connection status")
	}
	q.Arguments = json.RawMessage(`{"path":{"id":"../secret"}}`)
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("path escape")
	}
	if calls != 1 {
		t.Fatal("invalid path dispatched")
	}
	for _, ref := range []string{"https://internal/schema", "file:///etc/passwd", "#/components/schemas/Loop"} {
		spec := `{"openapi":"3.0.3","components":{"schemas":{"Loop":{"$ref":"` + ref + `"}}}}`
		if _, err := openAPITools(json.RawMessage(spec)); err == nil {
			t.Fatal("unsafe reference accepted")
		}
	}
}

func TestMCPDiscoveryCannotAuthorizeAnnotationsAndPinsSchema(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var calls atomic.Int32
	add := func(description string) {
		server.AddTool(&mcp.Tool{Name: "read_item", Description: description, InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture"}}}, nil
		})
	}
	add("Read fixture")
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	ts := httptest.NewTLSServer(h)
	defer ts.Close()
	m, err := newManager(filepath.Join(t.TempDir(), "catalog.json"), modelprofiles.NewMemoryCredentialStore(), ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	// The Linear preset reuses this exact protocol path but fixes its upstream
	// endpoint; the test transport redirects only this owned fixture's traffic.
	original := m.http.Transport
	local, _ := url.Parse(ts.URL)
	m.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		request = request.Clone(request.Context())
		target := *request.URL
		target.Scheme, target.Host = local.Scheme, local.Host
		request.URL = &target
		request.Host = target.Host
		return original.RoundTrip(request)
	})
	linear := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "linear", Name: "Linear fixture", Credential: "linear-fixture-key", Endpoint: "https://ignored.invalid"}}).Account
	if linear.Endpoint != "https://mcp.linear.app/mcp" || linear.Tools[0].Allowed {
		t.Fatal("Linear endpoint or grant boundary")
	}
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "mcp", Name: "fixture", Endpoint: ts.URL}}).Account
	q := Request{Action: "invoke", ID: a.ID, Tool: "read_item"}
	if _, err = m.Handle(context.Background(), q); err == nil {
		t.Fatal("MCP annotation granted authorization")
	}
	mustHandle(t, m, Request{Action: "policy", ID: a.ID, Tool: q.Tool, Allowed: true})
	mustHandle(t, m, q)
	if calls.Load() != 1 {
		t.Fatal("tool not invoked")
	}
	add("Changed behavior")
	if _, err = m.Handle(context.Background(), q); err == nil {
		t.Fatal("changed tool dispatched")
	}
	if calls.Load() != 1 {
		t.Fatal("changed tool reached handler")
	}
	mustHandle(t, m, Request{Action: "refresh", ID: a.ID})
	if _, err = m.Handle(context.Background(), q); err == nil {
		t.Fatal("refresh carried grant to changed tool")
	}
}

func TestEndpointBoundary(t *testing.T) {
	for _, s := range []string{"http://example.com", "https://token@example.com", "https://example.com?token=x", "file:///etc/passwd"} {
		if _, err := endpointURL(s); err == nil {
			t.Fatal("unsafe endpoint")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://127.0.0.1:443", nil)
	if _, err := newHTTPClient().Do(req); err == nil {
		t.Fatal("private host reachable")
	}
}

func TestNotionReadAndAuthorizedUpdateUseSameWorkspace(t *testing.T) {
	calls := 0
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer notion-fixture-secret" || r.Header.Get("Notion-Version") != "2022-06-28" {
			t.Fatal("Notion credential or version boundary")
		}
		switch r.URL.Path {
		case "/v1/users/me":
			return reply(200, `{"id":"bot-id","name":"Zen","bot":{"workspace_name":"Fixture workspace"}}`), nil
		case "/v1/search":
			if r.Method != "POST" {
				t.Fatal("search must use official POST API")
			}
			return reply(200, `{"results":[],"has_more":false}`), nil
		case "/v1/pages/page-id":
			if r.Method != "PATCH" {
				t.Fatal("update must use PATCH")
			}
			return reply(200, `{"id":"page-id","archived":true}`), nil
		default:
			t.Fatal("unexpected Notion operation")
			return nil, nil
		}
	})
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "notion", Name: "Notion fixture", Credential: "notion-fixture-secret"}}).Account
	if a.Identity != "Fixture workspace" {
		t.Fatal("workspace identity missing")
	}
	mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "search", Arguments: json.RawMessage(`{"body":{"page_size":10}}`)})
	write := Request{Action: "invoke", ID: a.ID, Tool: "update_page", Arguments: json.RawMessage(`{"path":{"id":"page-id"},"body":{"archived":true}}`)}
	if _, err := m.Handle(context.Background(), write); err == nil {
		t.Fatal("write allowed without grant")
	}
	mustHandle(t, m, Request{Action: "policy", ID: a.ID, Tool: write.Tool, Allowed: true})
	mustHandle(t, m, write)
	if calls != 3 {
		t.Fatal("unexpected remote calls", calls)
	}
}

func TestSlackDiscoveryUsesActualScopesAndDoesNotAssumeBotSearch(t *testing.T) {
	for _, bot := range []bool{true, false} {
		m := testManager(t, func(r *http.Request) (*http.Response, error) {
			raw := `{"ok":true,"team":"Fixture","user":"Zen","user_id":"U1"}`
			if bot {
				raw = `{"ok":true,"team":"Fixture","user":"Zen","user_id":"U1","bot_id":"B1"}`
			}
			response := reply(200, raw)
			response.Header.Set("X-OAuth-Scopes", "channels:read,search:read,chat:write")
			return response, nil
		})
		a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "slack", Name: "Slack fixture", Credential: "slack-fixture-secret"}}).Account
		hasSearch := false
		for _, tool := range a.Tools {
			if tool.Name == "search_messages" {
				hasSearch = true
			}
			if tool.Name == "send_message" && tool.Allowed {
				t.Fatal("send auto-authorized")
			}
			if tool.Name == "channel_history" {
				t.Fatal("ungranted history advertised")
			}
		}
		if hasSearch == bot {
			t.Fatal("bot/user search scope boundary")
		}
		m.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return reply(200, `{"ok":false,"error":"slack-fixture-secret"}`), nil
		})
		if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: "get_me"}); err == nil || strings.Contains(err.Error(), "slack-fixture-secret") {
			t.Fatal("Slack application error leaked or ignored")
		}
	}
}
