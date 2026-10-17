package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

var emptyObject = map[string]any{"type": "object", "properties": map[string]any{}}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// mcpFixture is a custom MCP account backed by a stateless go-sdk server.
func mcpFixture(t *testing.T, server *mcp.Server) (*Manager, string) {
	t.Helper()
	ts := httptest.NewTLSServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(ts.Close)
	m := testManager(t, ts.Client().Transport.RoundTrip)
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "mcp", Name: "Fixture", Endpoint: ts.URL}}).Account
	return m, a.ID
}

func addFixtureTool(server *mcp.Server, name, description string, calls *atomic.Int32) {
	server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: emptyObject}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return textResult(name + " ran"), nil
	})
}

func TestToolUpdatesSyncWithoutBreakingTheConnection(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var steady, changing, fresh atomic.Int32
	addFixtureTool(server, "steady", "Unchanged", &steady)
	addFixtureTool(server, "changing", "Version one", &changing)
	addFixtureTool(server, "blocked", "Blocked by the person", &atomic.Int32{})
	m, id := mcpFixture(t, server)
	mustHandle(t, m, Request{Action: "policy", ID: id, Tools: []string{"steady", "changing"}, Allowed: true})

	// The service changes one allowed tool, one blocked tool, and adds one.
	addFixtureTool(server, "changing", "Version two", &changing)
	addFixtureTool(server, "blocked", "Blocked, but changed", &atomic.Int32{})
	addFixtureTool(server, "fresh", "New tool", &fresh)

	// Unchanged tools keep working, and the call adopts the new list.
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "steady"})
	a := mustHandle(t, m, Request{Action: "get", ID: id}).Account
	if a.Status != "connected" || strings.Join(a.Updates, ",") != "changing,fresh" {
		t.Fatalf("status %s updates %v", a.Status, a.Updates)
	}
	_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "changing"})
	if err == nil || !strings.Contains(err.Error(), "updated the tool") || strings.Contains(strings.ToLower(err.Error()), "authoriz") {
		t.Fatalf("updated tool error: %v", err)
	}
	if changing.Load() != 0 {
		t.Fatal("updated tool ran before confirmation")
	}
	a = mustHandle(t, m, Request{Action: "get", ID: id}).Account
	if a.Status != "connected" {
		t.Fatalf("a tool update marked the account %s", a.Status)
	}
	// The list shows the count without the tool list.
	list := mustHandle(t, m, Request{Action: "list"}).Accounts
	if len(list) != 1 || len(list[0].Updates) != 2 {
		t.Fatalf("list updates %v", list)
	}
	// Confirmation names what the person saw, all or none.
	if _, err = m.Handle(context.Background(), Request{Action: "confirm_updates", ID: id, Tools: []string{"changing", "steady"}}); err == nil {
		t.Fatal("confirmed a tool with no update")
	}
	mustHandle(t, m, Request{Action: "confirm_updates", ID: id, Tools: []string{"changing", "fresh"}})
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "changing"})
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "fresh"})
	if changing.Load() != 1 || fresh.Load() != 1 {
		t.Fatal("confirmed tools did not run")
	}
	a = mustHandle(t, m, Request{Action: "get", ID: id}).Account
	if len(a.Updates) != 0 {
		t.Fatalf("updates left %v", a.Updates)
	}
	for _, tool := range a.Tools {
		if tool.Name == "blocked" && (tool.Allowed || tool.Updated) {
			t.Fatal("a changed blocked tool became allowed or asked for confirmation")
		}
	}

	// Dismissing leaves an update blocked; a removed tool drops its grant.
	addFixtureTool(server, "fresh", "New tool, changed", &fresh)
	server.RemoveTools("steady")
	mustHandle(t, m, Request{Action: "refresh", ID: id})
	mustHandle(t, m, Request{Action: "dismiss_updates", ID: id, Tools: []string{"fresh"}})
	if _, err = m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "fresh"}); err == nil {
		t.Fatal("dismissed update ran")
	}
	m.mu.Lock()
	_, kept := m.records[id].Grants["steady"]
	m.mu.Unlock()
	if kept {
		t.Fatal("removed tool kept its grant")
	}
	// The pending set survives a restart.
	addFixtureTool(server, "changing", "Version three", &changing)
	mustHandle(t, m, Request{Action: "refresh", ID: id})
	reopened, err := newManager(m.path, m.vault, m.http)
	if err != nil {
		t.Fatal(err)
	}
	if a = mustHandle(t, reopened, Request{Action: "get", ID: id}).Account; strings.Join(a.Updates, ",") != "changing" {
		t.Fatalf("reopened updates %v", a.Updates)
	}
}

// The service removes, adds and changes tools between calls. Asking for
// updates shows all three without a call or refresh first.
func TestRemoteToolChangesShowWithoutACall(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var steady, changing, fresh, revert atomic.Int32
	addFixtureTool(server, "steady", "Unchanged", &steady)
	addFixtureTool(server, "changing", "Version one", &changing)
	addFixtureTool(server, "gone", "Removed soon", &atomic.Int32{})
	addFixtureTool(server, "revert", "Original", &revert)
	m, id := mcpFixture(t, server)
	mustHandle(t, m, Request{Action: "policy", ID: id, Tools: []string{"steady", "changing", "gone", "revert"}, Allowed: true})

	addFixtureTool(server, "changing", "Version two", &changing)
	server.RemoveTools("gone")
	addFixtureTool(server, "fresh", "New tool", &fresh)
	addFixtureTool(server, "revert", "Changed for a while", &revert)

	a := mustHandle(t, m, Request{Action: "updates", ID: id}).Account
	if strings.Join(a.Updates, ",") != "changing,fresh,revert" {
		t.Fatalf("updates %v", a.Updates)
	}
	tools := map[string]Tool{}
	for _, tool := range a.Tools {
		tools[tool.Name] = tool
	}
	if _, ok := tools["gone"]; ok {
		t.Fatal("a removed tool stayed in the list")
	}
	if c := tools["changing"]; c.Description != "Version two" || c.Allowed || !c.Updated {
		t.Fatalf("changed tool %+v", c)
	}
	if f := tools["fresh"]; f.Allowed || !f.Updated {
		t.Fatalf("new tool %+v", f)
	}
	if s := tools["steady"]; !s.Allowed || s.Updated {
		t.Fatalf("unchanged tool %+v", s)
	}
	m.mu.Lock()
	_, kept := m.records[id].Grants["gone"]
	m.mu.Unlock()
	if kept {
		t.Fatal("a removed tool kept its grant")
	}

	// A blocked call names the one command that confirms every waiting update.
	_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "changing"})
	want := `updated the tool "changing" since it was allowed`
	command := "mewla connections confirm-updates --id " + id + " --tools changing,fresh,revert"
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), command) || strings.Contains(strings.ToLower(err.Error()), "authoriz") {
		t.Fatalf("updated tool error: %v", err)
	}
	if _, err = m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "fresh"}); err == nil || !strings.Contains(err.Error(), `added the tool "fresh"`) {
		t.Fatalf("new tool error: %v", err)
	}
	if changing.Load()+fresh.Load() != 0 {
		t.Fatal("an unconfirmed tool ran")
	}

	// A tool that changes back to what the person allowed waits for nothing.
	addFixtureTool(server, "revert", "Original", &revert)
	a = mustHandle(t, m, Request{Action: "updates", ID: id}).Account
	if strings.Join(a.Updates, ",") != "changing,fresh" {
		t.Fatalf("updates after revert %v", a.Updates)
	}
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "revert"})

	// A tool removed after the list was read is reported as removed.
	server.RemoveTools("steady")
	_, err = m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "steady"})
	if err == nil || !strings.Contains(err.Error(), `no longer offers the tool "steady"`) {
		t.Fatalf("removed tool error: %v", err)
	}
	if steady.Load() != 0 {
		t.Fatal("a removed tool ran")
	}

	// A tool the saved list does not know yet is looked up before the call.
	addFixtureTool(server, "later", "Added after the last read", &atomic.Int32{})
	if _, err = m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "later"}); err == nil || !strings.Contains(err.Error(), `added the tool "later"`) {
		t.Fatalf("unknown tool error: %v", err)
	}

	// Describing reads the list again once it is older than a minute.
	addFixtureTool(server, "described", "Seen by describe", &atomic.Int32{})
	m.mu.Lock()
	m.toolsSeen[id] = time.Now().Add(-toolsFresh)
	m.mu.Unlock()
	if d := mustHandle(t, m, Request{Action: "describe", ID: id, Tool: "described"}).Tool; d == nil || !d.Updated {
		t.Fatalf("describe missed a new tool: %+v", d)
	}

	// Confirming the named set lets them run.
	mustHandle(t, m, Request{Action: "confirm_updates", ID: id, Tools: []string{"changing", "described", "fresh", "later"}})
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "changing"})
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "fresh"})
	if changing.Load() != 1 || fresh.Load() != 1 {
		t.Fatal("confirmed tools did not run")
	}
}

// Showing an account never refreshes an expired OAuth token, so it never
// holds the manager lock over the network; updates and calls do refresh.
func TestStoredTokenNeedsNoRefresh(t *testing.T) {
	m := testManager(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("no network") })
	r := &record{Account: Account{ID: "a1"}}
	_ = m.vault.Set("integration:a1", "key")
	if secret, ok := m.storedToken(r); !ok || secret != "key" {
		t.Fatal("an API key needs no refresh")
	}
	r.OAuth = &oauthAccount{}
	for _, c := range []struct {
		expiry time.Time
		ok     bool
	}{{time.Now().Add(time.Hour), true}, {time.Now().Add(-time.Hour), false}} {
		raw, _ := json.Marshal(oauthSecret{Token: &oauth2.Token{AccessToken: "at", RefreshToken: "rt", Expiry: c.expiry}})
		_ = m.vault.Set("integration:a1", string(raw))
		if secret, ok := m.storedToken(r); ok != c.ok || (ok && secret != "at") {
			t.Fatalf("expiry %v: ok %v", c.expiry, ok)
		}
	}
}

func TestToolUpdatedErrorQuotesRemoteNames(t *testing.T) {
	e := toolUpdatedError{service: "Svc", id: "a1", tool: "x", pending: []string{"x", "y;rm -rf $HOME"}}
	if !strings.Contains(e.Error(), `--tools 'x,y;rm -rf $HOME'`) {
		t.Fatalf("names not quoted: %s", e.Error())
	}
	e.pending = []string{"x", "a,b"}
	if strings.Contains(e.Error(), "confirm-updates") {
		t.Fatalf("a name with a comma cannot be confirmed by the command: %s", e.Error())
	}
}

// handRolledMCP answers just enough MCP to control what a call's response
// stream carries.
func handRolledMCP(t *testing.T, tools func() []map[string]any, call func(w http.ResponseWriter, id json.RawMessage)) *httptest.Server {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&msg) != nil {
			w.WriteHeader(400)
			return
		}
		respond := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		}
		switch msg.Method {
		case "initialize":
			respond(map[string]any{"protocolVersion": msg.Params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": true}}, "serverInfo": map[string]any{"name": "hand", "version": "1"}})
		case "tools/list":
			respond(map[string]any{"tools": tools()})
		case "tools/call":
			call(w, msg.ID)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestToolListChangedDuringACallIsAdopted(t *testing.T) {
	var mu sync.Mutex
	list := []map[string]any{{"name": "work", "description": "Does work", "inputSchema": emptyObject}}
	tools := func() []map[string]any { mu.Lock(); defer mu.Unlock(); return append([]map[string]any(nil), list...) }
	ts := handRolledMCP(t, tools, func(w http.ResponseWriter, id json.RawMessage) {
		mu.Lock()
		list = append(list, map[string]any{"name": "later", "description": "Added during the call", "inputSchema": emptyObject})
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		notice, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"})
		result, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "done"}}}})
		fmt.Fprintf(w, "event: message\ndata: %s\n\nevent: message\ndata: %s\n\n", notice, result)
	})
	m := testManager(t, ts.Client().Transport.RoundTrip)
	id := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "mcp", Name: "Hand", Endpoint: ts.URL}}).Account.ID
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "work", Allowed: true})
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "work"})
	if a := mustHandle(t, m, Request{Action: "get", ID: id}).Account; strings.Join(a.Updates, ",") != "later" {
		t.Fatalf("list_changed not adopted: %v", a.Updates)
	}
}

func TestServiceErrorsReachTheCaller(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "render", InputSchema: emptyObject}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		r := textResult(`{"error":{"code":"INVALID_WORK","message":"pages is required; token sk-fixture-secret"}}` + strings.Repeat("页", 2000))
		r.IsError = true
		return r, nil
	})
	m, id := mcpFixture(t, server)
	m.mu.Lock()
	_ = m.vault.Set("integration:"+id, "sk-fixture-secret")
	m.mu.Unlock()
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "render", Allowed: true})
	_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "render"})
	if err == nil || !strings.Contains(err.Error(), "INVALID_WORK") || !strings.Contains(err.Error(), "pages is required") {
		t.Fatalf("service error body lost: %v", err)
	}
	if strings.Contains(err.Error(), "sk-fixture-secret") || len(err.Error()) > maxServiceErrorBytes+200 || !strings.HasSuffix(err.Error(), "…") {
		t.Fatalf("error not redacted or cut: %d bytes", len(err.Error()))
	}
	if a := mustHandle(t, m, Request{Action: "get", ID: id}).Account; a.Status != "connected" || a.History[0].Status != "error" {
		t.Fatalf("service answer marked the account %s", a.Status)
	}

	// A JSON-RPC error keeps its code and message.
	ts := handRolledMCP(t, func() []map[string]any {
		return []map[string]any{{"name": "work", "description": "w", "inputSchema": emptyObject}}
	}, func(w http.ResponseWriter, id json.RawMessage) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "Invalid arguments: project_id must be a string"}})
	})
	m = testManager(t, ts.Client().Transport.RoundTrip)
	id = mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "mcp", Name: "Hand", Endpoint: ts.URL}}).Account.ID
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "work", Allowed: true})
	_, err = m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "work"})
	if err == nil || !strings.Contains(err.Error(), "project_id must be a string") || !strings.Contains(err.Error(), "-32602") {
		t.Fatalf("JSON-RPC error lost: %v", err)
	}
}

func TestMCPTimeoutFollowsTheRequestedWait(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"wait_s":{"type":"integer","maximum":45,"default":20}}}`)
	tool := Tool{Name: "get_job", InputSchema: schema}
	cases := []struct {
		args map[string]any
		want time.Duration
	}{
		{map[string]any{}, mcpCallTimeout},                          // default 20s + margin is under the base
		{map[string]any{"wait_s": float64(45)}, mcpCallTimeout},     // 75s, still under the base
		{map[string]any{"wait_s": float64(300)}, 330 * time.Second}, // longer than the base
		{map[string]any{"timeout_ms": float64(200_000)}, 230 * time.Second},
		{map[string]any{"wait_s": float64(1e9)}, MaxCallTimeout},
	}
	for _, c := range cases {
		if got := mcpTimeout(tool, c.args); got != c.want {
			t.Fatalf("%v: %s, want %s", c.args, got, c.want)
		}
	}
	if got := mcpTimeout(Tool{InputSchema: json.RawMessage(`{"properties":{"wait_s":{"default":400}}}`)}, map[string]any{}); got != 430*time.Second {
		t.Fatalf("schema default wait: %s", got)
	}
	if mcpCallTimeout <= 20*time.Second {
		t.Fatal("base MCP timeout must exceed the old 20s budget")
	}
}

func TestMCPTimeoutSaysTheCallMayHaveRunAndNeverRetries(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var calls atomic.Int32
	release := make(chan struct{})
	server.AddTool(&mcp.Tool{Name: "generate", InputSchema: emptyObject}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return textResult("finished late"), nil
	})
	m, id := mcpFixture(t, server)
	// The stateless fixture does not see the client leave; let it finish
	// before the server shuts down.
	t.Cleanup(func() { close(release) })
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "generate", Allowed: true})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := m.Handle(ctx, Request{Action: "invoke", ID: id, Tool: "generate"})
	if err == nil || !strings.Contains(err.Error(), "may already have run") || !strings.Contains(err.Error(), "did not retry") {
		t.Fatalf("timeout message: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("a timed out call was sent %d times", calls.Load())
	}
	if a := mustHandle(t, m, Request{Action: "get", ID: id}).Account; a.Status != "connected" {
		t.Fatalf("a slow answer marked the account %s", a.Status)
	}
}

func TestConcurrentCallsOnOneAccountRunSideBySide(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var inside atomic.Int32
	both := make(chan struct{})
	server.AddTool(&mcp.Tool{Name: "get_asset", InputSchema: emptyObject}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if inside.Add(1) == 2 {
			close(both)
		}
		// Each call waits until the other is running too.
		select {
		case <-both:
			return textResult("asset"), nil
		case <-time.After(5 * time.Second):
			return nil, errors.New("the other call never ran alongside")
		}
	})
	m, id := mcpFixture(t, server)
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "get_asset", Allowed: true})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "get_asset"})
		}()
	}
	// Other requests are not held up by the calls in flight.
	for inside.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	mustHandle(t, m, Request{Action: "list"})
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if a := mustHandle(t, m, Request{Action: "get", ID: id}).Account; len(a.History) < 2 || a.Status != "connected" {
		t.Fatalf("history %v status %s", a.History, a.Status)
	}
}

func TestMCPSetupRetriesOnceButNeverResendsACall(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	var calls atomic.Int32
	addFixtureTool(server, "write", "Writes", &calls)
	m, id := mcpFixture(t, server)
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "write", Allowed: true})
	base := m.http.Transport
	var initFailures, callFailures atomic.Int32
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		if r.Body != nil {
			raw, _ := r.GetBody()
			buf := new(strings.Builder)
			_, _ = fmt.Fprint(buf, readAll(raw))
			body = buf.String()
		}
		if strings.Contains(body, `"initialize"`) && initFailures.Add(1) == 1 {
			return nil, errors.New("dial tcp: i/o timeout")
		}
		if strings.Contains(body, `"tools/call"`) && callFailures.Load() > 0 {
			callFailures.Add(1)
			// Sent, then the connection broke before an answer.
			_, _ = base.RoundTrip(r)
			return nil, errors.New("connection reset by peer")
		}
		return base.RoundTrip(r)
	})
	mustHandle(t, m, Request{Action: "invoke", ID: id, Tool: "write"})
	if initFailures.Load() != 2 || calls.Load() != 1 {
		t.Fatalf("setup retry: init attempts %d, calls %d", initFailures.Load(), calls.Load())
	}
	callFailures.Store(1)
	_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "write"})
	if err == nil || !strings.Contains(err.Error(), "may already have run") || calls.Load() != 2 || callFailures.Load() != 2 {
		t.Fatalf("broken call: %v, calls %d, attempts %d", err, calls.Load(), callFailures.Load()-1)
	}
}

func readAll(r interface{ Read([]byte) (int, error) }) string {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}

func TestDialTriesEachCheckedAddress(t *testing.T) {
	var tried []string
	conn, err := dialEach(context.Background(), "tcp", "443", []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")}, func(ctx context.Context, network, address string) (net.Conn, error) {
		tried = append(tried, address)
		if strings.HasPrefix(address, "203.0.113.1:") {
			return nil, errors.New("i/o timeout")
		}
		client, server := net.Pipe()
		server.Close()
		return client, nil
	})
	if err != nil || conn == nil || len(tried) != 2 {
		t.Fatalf("dial: %v %v", err, tried)
	}
	conn.Close()
}

func TestResourcesListAndReadBehindReadAccess(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "Fixture", Version: "1"}, nil)
	addFixtureTool(server, "list", "Lists", &atomic.Int32{})
	server.AddResource(&mcp.Resource{URI: "fixture://docs/forms", Name: "forms", MIMEType: "text/markdown"}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "text/markdown", Text: "# Work forms"}}}, nil
	})
	server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "fixture://projects/{id}", Name: "project"}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: "{}"}}}, nil
	})
	m, id := mcpFixture(t, server)
	if _, err := m.Handle(context.Background(), Request{Action: "resources", ID: id}); err == nil {
		t.Fatal("resources readable with nothing allowed")
	}
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "list", Allowed: true})
	var list mcpResourceList
	if err := json.Unmarshal(mustHandle(t, m, Request{Action: "resources", ID: id}).Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Resources) != 1 || list.Resources[0].URI != "fixture://docs/forms" || len(list.Templates) != 1 || list.Templates[0].URITemplate != "fixture://projects/{id}" {
		t.Fatalf("list %+v", list)
	}
	raw := mustHandle(t, m, Request{Action: "read_resource", ID: id, URI: "fixture://docs/forms"}).Result
	if !strings.Contains(string(raw), "# Work forms") {
		t.Fatalf("read %s", raw)
	}
	if raw = mustHandle(t, m, Request{Action: "read_resource", ID: id, URI: "fixture://projects/p1"}).Result; !strings.Contains(string(raw), "fixture://projects/p1") {
		t.Fatalf("template read %s", raw)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "read_resource", ID: id, URI: "fixture://missing"}); err == nil {
		t.Fatal("missing resource read")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "read_resource", ID: id, URI: "no scheme"}); err == nil {
		t.Fatal("invalid URI accepted")
	}
	mustHandle(t, m, Request{Action: "disable", ID: id})
	if _, err := m.Handle(context.Background(), Request{Action: "resources", ID: id}); !errors.Is(err, ErrAccountOff) {
		t.Fatalf("disabled account: %v", err)
	}
}
