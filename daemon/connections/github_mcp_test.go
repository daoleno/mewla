package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// githubFixture is GitHub's MCP server in miniature: a go-sdk server on a TLS
// test server, reached under api.githubcopilot.com through the manager's
// transport. It records what each request carried.
type githubFixture struct {
	t      *testing.T
	server *mcp.Server
	ts     *httptest.Server
	login  string
	writes atomic.Int32

	mu       sync.Mutex
	requests []githubRequest
}

type githubRequest struct{ Host, Path, Authorization, Toolsets string }

// Input schemas as GitHub serves them, with its x-mcp-header keyword.
const githubRepoSchema = `{"type":"object","properties":{"method":{"type":"string","enum":["list_workflows","list_workflow_runs"]},"owner":{"type":"string","x-mcp-header":"owner"},"repo":{"type":"string","x-mcp-header":"repo"},"perPage":{"type":"number","minimum":1,"maximum":100}},"required":["method","owner","repo"]}`

func newGitHubFixture(t *testing.T, login string) *githubFixture {
	t.Helper()
	f := &githubFixture{t: t, login: login}
	f.server = mcp.NewServer(&mcp.Implementation{Name: "github-mcp-server", Version: "2"}, nil)
	f.addTool("get_me", `{"type":"object","properties":{}}`, func() string { return `{"login":"` + f.login + `","id":42}` }, nil)
	f.addTool("actions_list", githubRepoSchema, func() string { return `{"total_count":0,"workflow_runs":[]}` }, nil)
	f.addTool("issue_write", `{"type":"object","properties":{"method":{"type":"string"},"owner":{"type":"string"},"repo":{"type":"string"},"title":{"type":"string"}},"required":["method","owner","repo"]}`, func() string { return `{"id":1}` }, &f.writes)
	f.ts = httptest.NewTLSServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return f.server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *githubFixture) addTool(name, schema string, body func() string, count *atomic.Int32) {
	f.server.AddTool(&mcp.Tool{Name: name, Description: name + " description", InputSchema: json.RawMessage(schema)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if count != nil {
			count.Add(1)
		}
		return textResult(body()), nil
	})
}

// route sends api.githubcopilot.com to the fixture and everything else to
// other, which a test sets when its flow also talks to github.com.
func (f *githubFixture) route(other roundTripFunc) roundTripFunc {
	target, _ := url.Parse(f.ts.URL)
	return func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.githubcopilot.com" {
			if other == nil {
				f.t.Fatalf("unexpected request %s", r.URL)
			}
			return other(r)
		}
		f.mu.Lock()
		f.requests = append(f.requests, githubRequest{r.URL.Host, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-MCP-Toolsets")})
		f.mu.Unlock()
		c := r.Clone(r.Context())
		c.URL.Scheme, c.URL.Host, c.Host = target.Scheme, target.Host, ""
		return f.ts.Client().Transport.RoundTrip(c)
	}
}

func (f *githubFixture) seen() []githubRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]githubRequest(nil), f.requests...)
}

func (f *githubFixture) manager(t *testing.T) *Manager { return testManager(t, f.route(nil)) }

func addGitHub(t *testing.T, m *Manager, name, secret string) *Account {
	t.Helper()
	return mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "github", Name: name, Credential: secret}}).Account
}

func toolState(a *Account, name string) (Tool, bool) {
	for _, tool := range a.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

// An account speaks GitHub's own MCP server: every request carries the token
// and the reviewed toolsets, the account is named by its login, tools and
// descriptions are the server's, reads are on, and a change is refused until
// it is allowed.
func TestGitHubAccountSpeaksTheOfficialMCPServer(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	m := f.manager(t)
	a := addGitHub(t, m, "work", "gh-token")
	if a.Identity != "octo" || a.Endpoint != githubMCP || a.Status != "connected" {
		t.Fatalf("account %+v", a)
	}
	for _, request := range f.seen() {
		if request.Path != "/mcp/" || request.Authorization != "Bearer gh-token" || request.Toolsets != githubToolsets {
			t.Fatalf("request %+v", request)
		}
	}
	if len(a.Tools) != 3 {
		t.Fatalf("tools are the server's: %+v", a.Tools)
	}
	if tool, _ := toolState(a, "actions_list"); tool.Description != "actions_list description" || !strings.Contains(string(tool.InputSchema), "x-mcp-header") {
		t.Fatalf("the server's own definition was not kept: %+v", tool)
	}
	// Reads are reviewed as reads and a change as a change; neither is on by
	// itself for an account added with a token.
	if read, _ := toolState(a, "get_me"); read.Group != "read" {
		t.Fatalf("get_me is %q", read.Group)
	}
	if write, _ := toolState(a, "issue_write"); write.Group != "write" || write.Allowed {
		t.Fatalf("issue_write %+v", write)
	}
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "read", Allowed: true})
	got := mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "actions_list", Arguments: json.RawMessage(`{"method":"list_workflow_runs","owner":"o","repo":"r","perPage":5}`)})
	if !strings.Contains(string(got.Result), "workflow_runs") {
		t.Fatalf("read result %s", got.Result)
	}
	// A write is stopped before it reaches GitHub until the person allows it.
	_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: "issue_write", Arguments: json.RawMessage(`{"method":"create","owner":"o","repo":"r","title":"t"}`)})
	if err == nil || !strings.Contains(err.Error(), "not authorized") || f.writes.Load() != 0 {
		t.Fatalf("unauthorized write: %v (%d reached the server)", err, f.writes.Load())
	}
	mustHandle(t, m, Request{Action: "policy", ID: a.ID, Tool: "issue_write", Allowed: true})
	mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "issue_write", Arguments: json.RawMessage(`{"method":"create","owner":"o","repo":"r","title":"t"}`)})
	if f.writes.Load() != 1 {
		t.Fatal("an allowed write did not run")
	}
}

// A tool GitHub adds is not one Mewla has reviewed: it has no class and cannot
// be allowed by the read or change switches.
func TestGitHubToolsOutsideTheReviewedListHaveNoClass(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	f.addTool("brand_new_tool", `{"type":"object","properties":{}}`, func() string { return `{}` }, nil)
	m := f.manager(t)
	a := addGitHub(t, m, "work", "gh-token")
	tool, ok := toolState(a, "brand_new_tool")
	if !ok || tool.Group != "" {
		t.Fatalf("unreviewed tool %+v", tool)
	}
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "read", Allowed: true})
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "write", Allowed: true})
	if got := mustHandle(t, m, Request{Action: "get", ID: a.ID}).Account; func() bool { t, _ := toolState(got, "brand_new_tool"); return t.Allowed }() {
		t.Fatal("a group switch allowed an unreviewed tool")
	}
}

// The toolset header belongs to GitHub's endpoint alone.
func TestGitHubToolsetsAreSentOnlyToGitHub(t *testing.T) {
	e := lookup("github")
	if e.MCP.Headers["X-MCP-Toolsets"] != githubToolsets || e.MCP.Endpoint != e.Endpoint {
		t.Fatalf("github entry %+v", e.MCP)
	}
	for _, other := range integrations {
		if other.ID != "github" && other.MCP != nil && len(other.MCP.Headers) != 0 {
			t.Errorf("%s sends headers", other.ID)
		}
	}
	// A GitHub account that points elsewhere gets neither the headers nor an
	// official tool list.
	m := testManager(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("no network") })
	r := &record{Account: Account{Integration: "github", Endpoint: "https://other.example/mcp"}}
	client, _ := m.mcpClient(r, "tok")
	transport := client.Transport.(bearerTransport)
	if len(transport.headers) != 0 {
		t.Fatalf("headers %v went to another endpoint", transport.headers)
	}
	if toolGroup(r, Tool{Name: "get_me"}) != "" {
		t.Fatal("official list applied to another endpoint")
	}
}

// Every tool GitHub's served toolsets offer is classified once, and the
// identity tool is a read.
func TestGitHubReviewedToolsAreClassifiedOnce(t *testing.T) {
	spec := lookup("github").MCP
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, spec.Reads...), spec.Writes...) {
		if seen[name] {
			t.Errorf("%s is listed twice", name)
		}
		seen[name] = true
	}
	if spec.group(spec.Identity) != "read" || len(spec.Reads)+len(spec.Writes) != 54 {
		t.Fatalf("identity %q, %d tools", spec.Identity, len(spec.Reads)+len(spec.Writes))
	}
	for _, name := range []string{"actions_run_trigger", "issue_write", "merge_pull_request", "create_repository", "delete_file"} {
		if spec.group(name) != "write" {
			t.Errorf("%s is %q, not write", name, spec.group(name))
		}
	}
}

// A refusal GitHub writes (a feature switched off for a repository) is the
// service's answer, not a fault in reaching it.
func TestGitHubRefusalIsAServiceAnswer(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	f.server.AddTool(&mcp.Tool{Name: "list_dependabot_alerts", Description: "alerts", InputSchema: json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"}},"required":["owner","repo"]}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result := textResult("failed to list alerts: 403 Dependabot alerts are disabled for this repository.")
		result.IsError = true
		return result, nil
	})
	m := f.manager(t)
	a := addGitHub(t, m, "work", "gh-token")
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "read", Allowed: true})
	_, err := m.ReadTool(context.Background(), a.ID, "list_dependabot_alerts", map[string]any{"owner": "o", "repo": "r"})
	var answer ServiceError
	if !errors.As(err, &answer) || !strings.Contains(answer.Detail, "Dependabot alerts are disabled") {
		t.Fatalf("refusal: %v", err)
	}
	if got := mustHandle(t, m, Request{Action: "get", ID: a.ID}).Account; got.Status != "connected" {
		t.Fatalf("a refusal marked the account %s", got.Status)
	}
}

// An account made with the former hand-written REST tools keeps its token and
// moves to the official server: the reads it had on are on again, changes are
// not carried over, and nothing waits for confirmation.
func TestFormerGitHubAccountMovesToTheOfficialServer(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	m := f.manager(t)
	m.records["old"] = &record{
		Account: Account{ID: "old", Integration: "github", Name: "octo", Identity: "octo", Endpoint: githubRESTEndpoint, Enabled: true, Status: "connected",
			Tools: []Tool{{Name: "get_me", Description: "Read the authenticated GitHub account"}, {Name: "create_issue", Description: "Create a repository issue"}}},
		Grants: map[string]string{"get_me": "old-read", "create_issue": "old-write"},
	}
	m.records["off"] = &record{
		Account: Account{ID: "off", Integration: "github", Name: "quiet", Identity: "quiet", Endpoint: githubRESTEndpoint, Enabled: true, Status: "connected"},
		Grants:  map[string]string{"create_issue": "old-write"},
	}
	if err := m.vault.Set("integration:old", "stored-token"); err != nil {
		t.Fatal(err)
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newManager(m.path, m.vault, m.http)
	if err != nil {
		t.Fatal(err)
	}
	moved := reopened.records["old"]
	if moved.Account.Endpoint != githubMCP || len(moved.Account.Tools) != 0 || len(moved.Grants) != 0 || !moved.Moved {
		t.Fatalf("moved account %+v", moved)
	}
	// A background reader finds the official tools by itself.
	body, err := reopened.ReadTool(context.Background(), "old", "actions_list", map[string]any{"method": "list_workflow_runs", "owner": "o", "repo": "r"})
	if err != nil || !strings.Contains(string(body.Body), "workflow_runs") {
		t.Fatalf("read after the move: %v %s", err, body.Body)
	}
	for _, request := range f.seen() {
		if request.Authorization != "Bearer stored-token" || request.Toolsets != githubToolsets {
			t.Fatalf("request %+v", request)
		}
	}
	a := mustHandle(t, reopened, Request{Action: "get", ID: "old"}).Account
	if len(a.Updates) != 0 || a.Status != "connected" || len(a.Tools) != 3 {
		t.Fatalf("updates %v status %s tools %d", a.Updates, a.Status, len(a.Tools))
	}
	if read, _ := toolState(a, "get_me"); !read.Allowed {
		t.Fatal("the reads the person had on are off")
	}
	if write, _ := toolState(a, "issue_write"); write.Allowed {
		t.Fatal("a change was carried over to a different tool")
	}
	// The move is saved: another start does not repeat it or lose the grants.
	again, err := newManager(reopened.path, reopened.vault, reopened.http)
	if err != nil {
		t.Fatal(err)
	}
	if r := again.records["old"]; r.Moved || r.ReadsOn || len(r.Grants) == 0 || len(r.Account.Tools) != 3 {
		t.Fatalf("after another start %+v", r)
	}
	// An account that had no read on stays that way; GitHub refusing the stored
	// token asks the person to sign in again.
	if r := again.records["off"]; !r.Moved || r.ReadsOn {
		t.Fatalf("quiet account %+v", r)
	}
	refusing := testManager(t, func(r *http.Request) (*http.Response, error) { return reply(401, `{}`), nil })
	refusing.records["old"] = &record{Account: Account{ID: "old", Integration: "github", Name: "octo", Endpoint: githubMCP, Enabled: true, Status: "connected"}, Grants: map[string]string{}, Moved: true, ReadsOn: true}
	if err := refusing.vault.Set("integration:old", "revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := refusing.ReadTool(context.Background(), "old", "get_me", map[string]any{}); err == nil {
		t.Fatal("a read with a refused token succeeded")
	}
	if got := refusing.records["old"].Account.Status; got == "connected" {
		t.Fatalf("a refused token left the account %s", got)
	}
}

// A moved account lists its official tools when the daemon starts, so it does
// not show an empty list until someone opens it.
func TestMovedAccountListsItsToolsAtStart(t *testing.T) {
	f := newGitHubFixture(t, "octo")
	m := f.manager(t)
	m.records["old"] = &record{Account: Account{ID: "old", Integration: "github", Name: "octo", Endpoint: githubMCP, Enabled: true, Status: "connected"}, Grants: map[string]string{}, Moved: true, ReadsOn: true}
	if err := m.vault.Set("integration:old", "stored-token"); err != nil {
		t.Fatal(err)
	}
	m.listMovedTools()
	m.mu.Lock()
	r := m.records["old"]
	tools, moved := len(r.Account.Tools), r.Moved
	m.mu.Unlock()
	if tools != 3 || moved {
		t.Fatalf("%d tools, moved %v", tools, moved)
	}
	if read, _ := toolState(mustHandle(t, m, Request{Action: "get", ID: "old"}).Account, "get_me"); !read.Allowed {
		t.Fatal("reads are off")
	}
}

// Importing the gh login over an account that moved but has not listed its
// tools yet settles the move: a later tool list does not turn reads back on
// after the person switched them off.
func TestImportSettlesAMovedAccount(t *testing.T) {
	m, _ := New(t.TempDir())
	m.networkOwned = false
	f := newGitHubFixture(t, "octo")
	m.http = &http.Client{Transport: f.route(nil)}
	m.githubToken = func(context.Context) (string, error) { return "gh-token", nil }
	m.records["old"] = &record{Account: Account{ID: "old", Integration: "github", Name: "octo", Identity: "octo", Endpoint: githubMCP, Enabled: true, Status: "connected"}, Grants: map[string]string{}, Moved: true, ReadsOn: true}
	preview := mustHandle(t, m, Request{Action: "github_preview"})
	imported := mustHandle(t, m, Request{Action: "github_import", FlowID: preview.Flow.ID}).Account
	if imported.ID != "old" || len(m.records) != 1 {
		t.Fatalf("import made another account: %+v", imported)
	}
	if r := m.records["old"]; r.Moved || r.ReadsOn {
		t.Fatalf("the move is still open: %+v", r)
	}
	mustHandle(t, m, Request{Action: "permissions", ID: "old", Group: "read", Allowed: false})
	mustHandle(t, m, Request{Action: "refresh", ID: "old"})
	if read, _ := toolState(mustHandle(t, m, Request{Action: "get", ID: "old"}).Account, "get_me"); read.Allowed {
		t.Fatal("a tool list turned reads back on")
	}
}
