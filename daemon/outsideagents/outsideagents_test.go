package outsideagents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeBackend struct {
	submitted []string
	err       error
}

func (f *fakeBackend) ListWork() ([]WorkSummary, error) {
	return []WorkSummary{{ID: "w1", Title: "Ship it", Status: "running"}, {ID: "w2", Title: "Old", Status: "done"}}, nil
}
func (f *fakeBackend) Work(id string) (WorkSummary, []WorkEventSummary, error) {
	if id != "w1" {
		return WorkSummary{}, nil, errors.New("no such work")
	}
	return WorkSummary{ID: "w1", Title: "Ship it", Status: "running"}, []WorkEventSummary{{Kind: "progress", Summary: "half way"}}, nil
}
func (f *fakeBackend) SubmitToBrain(receipt, body string) (string, error) {
	f.submitted = append(f.submitted, receipt+"\n"+body)
	if f.err != nil {
		return "not_submitted", f.err
	}
	return "accepted", nil
}

func approvedAgent(t *testing.T, m *Manager, name string) (string, Agent) {
	t.Helper()
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := m.Connect(name, HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decide("", pending.Number, true, "test"); err != nil {
		t.Fatal(err)
	}
	agent, ok := m.Authenticate(token)
	if !ok {
		t.Fatal("approved token not accepted")
	}
	return token, agent
}

func TestApprovalIsRequiredAndRevocable(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, _ := NewToken()
	pending, err := m.Connect("Claude Code\x1b[2J \"x\" <b>", HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(pending.Name, "\x1b\"<>") {
		t.Fatalf("name not cleaned: %q", pending.Name)
	}
	if _, ok := m.Authenticate(token); ok {
		t.Fatal("a pending agent authenticated before approval")
	}
	if _, err := m.Decide("", pending.Number, true, "phone"); err != nil {
		t.Fatal(err)
	}
	agent, ok := m.Authenticate(token)
	if !ok {
		t.Fatal("approved agent refused")
	}
	if _, ok := m.Authenticate("mwa_" + strings.Repeat("0", 64)); ok {
		t.Fatal("unknown token accepted")
	}
	if err := m.Revoke(agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Authenticate(token); ok {
		t.Fatal("revoked token still works")
	}
}

func TestDeniedAndExpiredNeverAuthenticate(t *testing.T) {
	m, _ := New(t.TempDir())
	clock := time.Now()
	m.now = func() time.Time { return clock }
	deny, _ := NewToken()
	p1, _ := m.Connect("A", HashToken(deny))
	if _, err := m.Decide(p1.ID, "", false, "x"); err != nil {
		t.Fatal(err)
	}
	late, _ := NewToken()
	p2, _ := m.Connect("B", HashToken(late))
	clock = clock.Add(ApprovalTTL + time.Second)
	if _, err := m.Decide(p2.ID, "", true, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired request could be approved: %v", err)
	}
	for _, token := range []string{deny, late} {
		if _, ok := m.Authenticate(token); ok {
			t.Fatal("a denied or expired request authenticated")
		}
	}
	status, err := m.Status(p2.ID)
	if err != nil || status.Status != StatusExpired {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m, _ := New(dir)
	token, _ := approvedAgent(t, m, "Codex")
	again, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Authenticate(token); !ok {
		t.Fatal("approval lost across restart")
	}
}

func TestDoorTransportAndAuth(t *testing.T) {
	m, _ := New(t.TempDir())
	door := NewDoor(m, &fakeBackend{})
	token, _ := approvedAgent(t, m, "Claude Code")

	do := func(host, remote, auth string, tls bool) int {
		r := httptest.NewRequest("POST", "http://"+host+"/mcp", strings.NewReader(`{}`))
		r.RemoteAddr = remote
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		if tls {
			r.TLS = &tlsState
		}
		w := httptest.NewRecorder()
		door.ServeHTTP(w, r)
		return w.Code
	}
	if got := do("192.168.1.5:9876", "192.168.1.9:5000", "Bearer "+token, false); got != 404 {
		t.Fatalf("plain LAN HTTP = %d, want 404", got)
	}
	if got := do("mewla.example.com", "127.0.0.1:5000", "Bearer "+token, false); got != 404 {
		t.Fatalf("loopback proxy with public host = %d, want 404", got)
	}
	if got := do("127.0.0.1:9876", "127.0.0.1:5000", "", false); got != 401 {
		t.Fatalf("no token = %d, want 401", got)
	}
	if got := do("127.0.0.1:9876", "127.0.0.1:5000", "Bearer mwa_bad", false); got != 401 {
		t.Fatalf("bad token = %d, want 401", got)
	}
	if got := do("quiet-otter.m.mewla.ai", "10.0.0.1:5000", "", true); got != 401 {
		t.Fatalf("TLS without token = %d, want 401 (reached the door)", got)
	}
}

func TestAgentToolsOverMCP(t *testing.T) {
	m, _ := New(t.TempDir())
	backend := &fakeBackend{}
	token, _ := approvedAgent(t, m, "Claude Code")
	httpServer := httptest.NewServer(http.HandlerFunc(NewDoor(m, backend).ServeHTTP))
	defer httpServer.Close()

	transport := &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Transport: bearer{token}}}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			raw, _ := json.Marshal(result.Content)
			t.Fatalf("%s failed: %s", name, raw)
		}
		out, _ := result.StructuredContent.(map[string]any)
		return out
	}
	list := call("list_work", nil)
	if work := list["work"].([]any); len(work) != 1 {
		t.Fatalf("open Work = %v; want only the running one", work)
	}
	asked := call("ask_brain", map[string]any{"request": "Summarize the failing tests"})
	id := asked["request_id"].(string)
	if asked["delivered"] != true || len(backend.submitted) != 1 {
		t.Fatalf("ask_brain = %v, submitted %v", asked, backend.submitted)
	}
	body := backend.submitted[0]
	for _, want := range []string{"<mewla_agent_message", `agent="Claude Code"`, "not type", "mewla agents mcp reply " + id} {
		if !strings.Contains(body, want) {
			t.Fatalf("Brain envelope lacks %q:\n%s", want, body)
		}
	}
	if got := call("get_request", map[string]any{"request_id": id}); got["note"] == "Brain has replied." {
		t.Fatal("reply reported before Brain replied")
	}
	if _, err := m.Reply(id, "Two tests fail: A and B.", "w1"); err != nil {
		t.Fatal(err)
	}
	got := call("get_request", map[string]any{"request_id": id})
	if got["note"] != "Brain has replied." || got["work"] == nil {
		t.Fatalf("get_request after reply = %v", got)
	}
	if follow := call("send_followup", map[string]any{"request_id": id, "message": "Only B please"}); follow["delivered"] != true || len(backend.submitted) != 2 {
		t.Fatalf("followup = %v", follow)
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "start_worker"}); err == nil {
		t.Fatal("the door exposes a tool that is not one of the five")
	}
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("tools = %v, %v; want exactly 5", tools, err)
	}
}

func TestOtherAgentsRequestsAreInvisibleAndRateLimited(t *testing.T) {
	m, _ := New(t.TempDir())
	_, a := approvedAgent(t, m, "A")
	_, b := approvedAgent(t, m, "B")
	req, err := m.Record(a, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Request(b.ID, req.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("agent B read agent A's request")
	}
	if _, _, err := m.Followup(b, req.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatal("agent B followed up on agent A's request")
	}
	for i := 0; i < requestsPerHour; i++ {
		if _, err := m.Record(b, "again"); err != nil {
			if i < requestsPerHour-1 {
				t.Fatalf("limited early at %d: %v", i, err)
			}
		}
	}
	if _, err := m.Record(b, "one too many"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want rate limit", err)
	}
	if _, err := m.Record(a, strings.Repeat("x", MaxTextBytes+1)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v, want too long", err)
	}
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

var tlsState = tlsConnectionState()

// A request that Mewla Relay forwards arrives over TLS from a loopback peer with
// the public host name; the SDK's rebinding guard must not refuse it.
func TestDoorServesTLSWithPublicHostFromLoopback(t *testing.T) {
	m, _ := New(t.TempDir())
	token, _ := approvedAgent(t, m, "Remote Claude")
	server := httptest.NewTLSServer(http.HandlerFunc(NewDoor(m, &fakeBackend{}).ServeHTTP))
	defer server.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(body))
	req.Host = "quiet-otter.m.mewla.ai"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize over TLS with a public Host = %d, want 200", resp.StatusCode)
	}
}
