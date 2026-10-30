package connections

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- fixture ------------------------------------------------------------

const awsTestPort = 9877

var awsRedirect = "http://127.0.0.1:9877/plugins/oauth/callback"

const awsTestARN = "arn:aws:sts::113732353381:assumed-role/AccountFullAccessRole/54784408-0061-700a-cbe9-387d91b9b747"

// awsCallLog is one call_boto3 request the fixture served.
type awsCallLog struct {
	Service, Operation, Region string
	Params                     map[string]any
}

// awsFixture stands in for AWS Sign-In and AWS's MCP server. Every request of
// the manager under test goes to it, whatever host it names.
type awsFixture struct {
	t  *testing.T
	mu sync.Mutex

	registered   []map[string]any
	clients      int
	challenges   map[string]string // code -> challenge
	access       string
	refresh      string
	generation   int
	refreshes    int
	refreshDelay time.Duration
	tokenError   string // an OAuth error code the token endpoint answers with

	account     string // the AWS account's name; "" when denied
	arn         string
	calls       []awsCallLog
	ops         map[string]func(params map[string]any) (any, string)
	working     bool // run_script answers "working" once, then get_tasks answers
	scriptError string
	taskCalls   int
	server      *httptest.Server
}

func newAWSFixture(t *testing.T) *awsFixture {
	f := &awsFixture{t: t, challenges: map[string]string{}, account: "Blank Canvas", arn: awsTestARN, ops: map[string]func(map[string]any) (any, string){}}
	f.rotate()
	f.ops["ec2/DescribeInstances"] = func(map[string]any) (any, string) {
		return map[string]any{"Reservations": []any{map[string]any{"Instances": []any{map[string]any{"InstanceId": "i-123", "UserData": "c2VjcmV0"}}}}}, ""
	}
	f.ops["ec2/DescribeRegions"] = func(map[string]any) (any, string) {
		return nil, "An error occurred (UnauthorizedOperation) when calling the DescribeRegions operation: You are not authorized to perform this operation. User: " + awsTestARN + " is not authorized to perform: ec2:DescribeRegions with an explicit deny in a service control policy"
	}
	f.ops["ec2/RunInstances"] = func(map[string]any) (any, string) { return map[string]any{"Instances": []any{}}, "" }
	f.ops["bedrock/ListFoundationModels"] = func(map[string]any) (any, string) {
		return map[string]any{"modelSummaries": []any{map[string]any{"modelId": "anthropic.claude"}}}, ""
	}
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *awsFixture) rotate() {
	f.generation++
	f.access = "aws-access-secret-" + string(rune('a'+f.generation))
	f.refresh = "aws-refresh-secret-" + string(rune('a'+f.generation))
}

// manager is a Manager whose traffic all goes to the fixture.
func (f *awsFixture) manager() *Manager {
	m, err := newManager(filepath.Join(f.t.TempDir(), "catalog.json"), modelprofiles.NewMemoryCredentialStore(), f.server.Client())
	if err != nil {
		f.t.Fatal(err)
	}
	local, _ := url.Parse(f.server.URL)
	original := m.http.Transport
	m.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("X-Original-Host", r.URL.Host)
		target := *r.URL
		target.Scheme, target.Host = local.Scheme, local.Host
		r.URL, r.Host = &target, target.Host
		return original.RoundTrip(r)
	})
	m.SetLoopbackPort(awsTestPort)
	return m
}

func (f *awsFixture) serve(w http.ResponseWriter, r *http.Request) {
	host := r.Header.Get("X-Original-Host")
	switch {
	case host == "us-east-1.oauth.signin.aws" && r.URL.Path == "/v1/register":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.registered = append(f.registered, body)
		f.clients++
		id := "arn:aws:signin:us-east-1::external-client/dcr/client-" + string(rune('0'+f.clients))
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": id, "token_endpoint_auth_method": "none"})
	case host == "us-east-1.oauth.signin.aws" && r.URL.Path == "/v1/token":
		f.token(w, r)
	case host == "aws-mcp.us-east-1.api.aws" && r.URL.Path == "/mcp":
		f.mu.Lock()
		ok := r.Header.Get("Authorization") == "Bearer "+f.access
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mcp().ServeHTTP(w, r)
	default:
		f.t.Errorf("unexpected request %s %s%s", r.Method, host, r.URL.Path)
		http.NotFound(w, r)
	}
}

func (f *awsFixture) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	w.Header().Set("Content-Type", "application/json")
	fail := func(code string) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": code})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Form.Get("resource") != awsMCPEndpoint {
		fail("invalid_target")
		return
	}
	if f.tokenError != "" {
		fail(f.tokenError)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		challenge, ok := f.challenges[r.Form.Get("code")]
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("redirect_uri") != awsRedirect {
			fail("invalid_grant")
			return
		}
		delete(f.challenges, r.Form.Get("code"))
	case "refresh_token":
		f.refreshes++
		if f.refreshDelay > 0 {
			f.mu.Unlock()
			time.Sleep(f.refreshDelay)
			f.mu.Lock()
		}
		if r.Form.Get("refresh_token") != f.refresh {
			fail("invalid_grant")
			return
		}
		f.rotate()
	default:
		fail("unsupported_grant_type")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.access, "refresh_token": f.refresh, "token_type": "Bearer", "expires_in": 3600})
}

// grant gives the code a browser would return for an authorization URL.
func (f *awsFixture) grant(authURL string) (state, code string) {
	u, err := url.Parse(authURL)
	if err != nil {
		f.t.Fatal(err)
	}
	q := u.Query()
	f.mu.Lock()
	defer f.mu.Unlock()
	code = "code-" + q.Get("state")[:6]
	f.challenges[code] = q.Get("code_challenge")
	return q.Get("state"), code
}

func (f *awsFixture) mcp() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "AWSMCP", Version: "1"}, nil)
	for _, name := range []string{"get_presigned_url", "list_regions", "get_regional_availability", "retrieve_skill"} {
		server.AddTool(&mcp.Tool{Name: "aws___" + name, InputSchema: object(map[string]any{})}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "{}"}}}, nil
		})
	}
	answer := func(v any) (*mcp.CallToolResult, error) {
		raw, _ := json.Marshal(v)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: v}, nil
	}
	server.AddTool(&mcp.Tool{Name: awsToolRunScript, InputSchema: object(map[string]any{"code": map[string]any{"type": "string"}}, "code")}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(req.Params.Arguments, &args)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.working {
			f.working = false
			return answer(map[string]any{"status": "working", "task_id": "task-1", "recommended_wait_seconds": 0.01})
		}
		if f.scriptError != "" {
			return answer(map[string]any{"status": "error", "error": f.scriptError})
		}
		return answer(map[string]any{"status": "success", "stdout": "", "stderr": "", "return_value": f.run(args.Code)})
	})
	server.AddTool(&mcp.Tool{Name: awsToolTasks, InputSchema: object(map[string]any{"task_ids": map[string]any{"type": "array"}}, "task_ids")}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.taskCalls++
		return answer(map[string]any{"status": "success", "return_value": map[string]any{"ok": true, "data": map[string]any{"Done": true}}})
	})
	server.AddTool(&mcp.Tool{Name: awsToolSearch, InputSchema: object(map[string]any{"search_phrase": map[string]any{"type": "string"}}, "search_phrase")}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return answer(map[string]any{"results": []any{map[string]any{"url": "https://docs.aws.amazon.com/rds/x", "context": "DescribeDBInstances"}}})
	})
	server.AddTool(&mcp.Tool{Name: awsToolRead, InputSchema: object(map[string]any{"requests": map[string]any{"type": "array"}})}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "# DescribeDBInstances\nReturns a list."}}}, nil
	})
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
}

// run answers the way the sandbox does for the two scripts Mewla sends. f.mu is held.
func (f *awsFixture) run(code string) any {
	one := func(service, operation, region string, params map[string]any) any {
		f.calls = append(f.calls, awsCallLog{service, operation, region, params})
		if service == "sts" && operation == "GetCallerIdentity" {
			return map[string]any{"ok": true, "data": map[string]any{"UserId": "AROA:me", "Account": "113732353381", "Arn": f.arn}}
		}
		if service == "account" && operation == "GetAccountInformation" {
			if f.account == "" {
				return map[string]any{"ok": false, "error": "AccessDenied: explicit deny in a service control policy"}
			}
			return map[string]any{"ok": true, "data": map[string]any{"AccountName": f.account, "AccountState": "ACTIVE"}}
		}
		if service == "iam" && operation == "ListAccountAliases" {
			return map[string]any{"ok": true, "data": map[string]any{"AccountAliases": []any{}}}
		}
		fn := f.ops[service+"/"+operation]
		if fn == nil {
			return map[string]any{"ok": false, "error": "unknown operation " + service + "/" + operation}
		}
		data, errText := fn(params)
		if errText != "" {
			return map[string]any{"ok": false, "error": errText}
		}
		return map[string]any{"ok": true, "data": data}
	}
	if code == awsProbeScript {
		return map[string]any{
			"identity": one("sts", "GetCallerIdentity", "us-east-1", nil),
			"account":  one("account", "GetAccountInformation", "us-east-1", nil),
			"aliases":  one("iam", "ListAccountAliases", "us-east-1", nil),
		}
	}
	call := decodeAWSScript(f.t, code)
	return one(call.Service, call.Operation, call.Region, call.Params)
}

type awsScriptCall struct {
	Service   string         `json:"service"`
	Operation string         `json:"operation"`
	Region    string         `json:"region"`
	Params    map[string]any `json:"params"`
}

// decodeAWSScript reads back what a script would load: the one JSON string
// literal on its first line, which is valid JSON as well as valid Python.
func decodeAWSScript(t *testing.T, code string) awsScriptCall {
	t.Helper()
	first, _, _ := strings.Cut(code, "\n")
	if !strings.HasPrefix(first, "call = json.loads(") || !strings.HasSuffix(first, ")") {
		t.Fatalf("unexpected script start %q", first)
	}
	var text string
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(first, "call = json.loads("), ")")), &text); err != nil {
		t.Fatalf("the argument is not one string literal: %v", err)
	}
	var call awsScriptCall
	if err := json.Unmarshal([]byte(text), &call); err != nil {
		t.Fatal(err)
	}
	return call
}

// signIn connects an account through the browser path and returns it.
func (f *awsFixture) signIn(m *Manager, in *Input) *Account {
	f.t.Helper()
	if in == nil {
		in = &Input{Integration: "aws"}
	}
	start := mustHandle(f.t, m, Request{Action: "connect_start", Input: in}).Flow
	state, code := f.grant(start.AuthorizationURL)
	rec := httptest.NewRecorder()
	m.BrowserOAuthCallback(rec, httptest.NewRequest(http.MethodGet, awsRedirect+"?state="+state+"&code="+code, nil), false)
	if rec.Code != 200 {
		f.t.Fatalf("callback answered %d: %s", rec.Code, rec.Body.String())
	}
	done := mustHandle(f.t, m, Request{Action: "connect_status", FlowID: start.ID})
	if done.Flow.Status != "connected" || done.Account == nil {
		f.t.Fatalf("sign-in ended %+v", done.Flow)
	}
	return done.Account
}

func (f *awsFixture) lastCall() awsCallLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// ---- sign-in ------------------------------------------------------------

func TestAWSSignInByLoopback(t *testing.T) {
	f := newAWSFixture(t)
	m := f.manager()
	start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
	if start.Status != "waiting" || !start.AddressRequired || start.WebReturn {
		t.Fatalf("flow %+v", start)
	}
	u, _ := url.Parse(start.AuthorizationURL)
	q := u.Query()
	if u.Host != "us-east-1.oauth.signin.aws" || u.Path != "/v1/authorize" || q.Get("redirect_uri") != awsRedirect || q.Get("resource") != awsMCPEndpoint ||
		q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" || q.Get("scope") != "" || q.Get("client_id") == "" {
		t.Fatalf("authorization URL %s", start.AuthorizationURL)
	}
	if len(f.registered) != 1 || f.registered[0]["redirect_uris"].([]any)[0] != awsRedirect || f.registered[0]["token_endpoint_auth_method"] != "none" {
		t.Fatalf("registration %v", f.registered)
	}
	if got := time.Until(start.Expires); got < 50*time.Minute {
		t.Fatalf("the sign-in waits only %s", got)
	}

	// Waiting reports where it stands and never errors.
	if got := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID}).Flow; got.Status != "waiting" {
		t.Fatalf("asking again changed the flow: %+v", got)
	}
	state, code := f.grant(start.AuthorizationURL)
	rec := httptest.NewRecorder()
	m.BrowserOAuthCallback(rec, httptest.NewRequest(http.MethodGet, awsRedirect+"?state="+state+"&code="+code, nil), false)
	if rec.Code != 200 {
		t.Fatalf("callback %d %s", rec.Code, rec.Body.String())
	}
	for _, leaked := range []string{f.access, f.refresh, code} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatal("the browser page shows a secret")
		}
	}
	done := mustHandle(t, m, Request{Action: "connect_status", FlowID: start.ID})
	a := done.Account
	if done.Flow.Status != "connected" || a == nil || a.Status != "connected" || a.Name != "Blank Canvas" || a.Identity != awsTestARN || a.Region != "us-east-1" {
		t.Fatalf("account %+v", a)
	}
	if a.AWS == nil || a.AWS.AccountID != "113732353381" || a.AWS.Who != "AccountFullAccessRole" || a.AWS.AccountName != "Blank Canvas" {
		t.Fatalf("details %+v", a.AWS)
	}
	if a.Access.Read != "allowed" || a.Access.Write == "allowed" {
		t.Fatalf("access %+v", a.Access)
	}
	// One state finishes once.
	rec = httptest.NewRecorder()
	m.BrowserOAuthCallback(rec, httptest.NewRequest(http.MethodGet, awsRedirect+"?state="+state+"&code="+code, nil), false)
	if rec.Code != 400 || len(m.records) != 1 {
		t.Fatalf("a used state finished again: %d", rec.Code)
	}
	raw, _ := json.Marshal(m.projection(m.records[a.ID]))
	if strings.Contains(string(raw), f.access) || strings.Contains(string(raw), f.refresh) {
		t.Fatal("the account shows a token")
	}
}

func TestAWSSignInByPastedAddress(t *testing.T) {
	f := newAWSFixture(t)
	m := f.manager()
	start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
	state, code := f.grant(start.AuthorizationURL)
	good := awsRedirect + "?code=" + code + "&state=" + state
	for name, address := range map[string]string{
		"other port":     "http://127.0.0.1:9999/plugins/oauth/callback?code=" + code + "&state=" + state,
		"other host":     "http://localhost:9877/plugins/oauth/callback?code=" + code + "&state=" + state,
		"https":          "https://127.0.0.1:9877/plugins/oauth/callback?code=" + code + "&state=" + state,
		"other path":     "http://127.0.0.1:9877/other?code=" + code + "&state=" + state,
		"other state":    awsRedirect + "?code=" + code + "&state=" + state + "x",
		"no state":       awsRedirect + "?code=" + code,
		"two states":     awsRedirect + "?code=" + code + "&state=" + state + "&state=" + state,
		"two codes":      awsRedirect + "?code=" + code + "&code=" + code + "&state=" + state,
		"userinfo":       "http://me@127.0.0.1:9877/plugins/oauth/callback?code=" + code + "&state=" + state,
		"fragment":       good + "#x",
		"spaces":         good + " extra",
		"mewla scheme":   "mewla://plugins?code=" + code + "&state=" + state,
		"someone else's": "https://example.com/?code=" + code + "&state=" + state,
	} {
		if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: start.ID, Callback: address}); err == nil {
			t.Errorf("%s: the address was accepted", name)
		}
	}
	if len(m.pending) != 1 || m.connectFlows[start.ID].Status != "waiting" {
		t.Fatal("a rejected address used up the sign-in")
	}
	// A bare copy of the address bar, without the scheme, is fine.
	done := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID, Callback: " " + strings.TrimPrefix(good, "http://") + "\n"})
	if done.Flow.Status != "connected" || done.Account == nil || done.Account.Name != "Blank Canvas" {
		t.Fatalf("flow %+v", done.Flow)
	}
	// The browser reaching this server afterwards finds the state used.
	rec := httptest.NewRecorder()
	m.BrowserOAuthCallback(rec, httptest.NewRequest(http.MethodGet, good, nil), false)
	if rec.Code != 400 || len(m.records) != 1 {
		t.Fatalf("second finish: %d", rec.Code)
	}
}

func TestAWSSignInRefusals(t *testing.T) {
	f := newAWSFixture(t)
	m := f.manager()
	for name, tc := range map[string]struct {
		query string
		cause string
	}{
		"denied":   {"error=access_denied&state=%s", "Access was not granted"},
		"no code":  {"state=%s", "didn’t complete"},
		"bad code": {"code=forged&state=%s", "didn’t accept"},
	} {
		start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
		state, _ := f.grant(start.AuthorizationURL)
		address := awsRedirect + "?" + strings.Replace(tc.query, "%s", state, 1)
		got := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID, Callback: address}).Flow
		if got.Status != "failed" || !strings.Contains(got.Message, tc.cause) || len(m.records) != 0 {
			t.Errorf("%s: %+v, %d accounts", name, got, len(m.records))
		}
	}
	// The sign-in must be able to reach this server.
	bare := newAWSFixture(t).manager()
	bare.SetLoopbackPort(0)
	if _, err := bare.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws"}}); err == nil {
		t.Fatal("started without knowing the port")
	}
	// Adding by hand is not offered.
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "aws", Name: "x", Credential: "AKIAABCDEFGHIJKLMNOP"}}); err == nil {
		t.Fatal("AWS was added without signing in")
	}
}

func TestAWSRegistrationIsKeptAndReplaced(t *testing.T) {
	f := newAWSFixture(t)
	m := f.manager()
	client := func() string {
		start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
		u, _ := url.Parse(start.AuthorizationURL)
		mustHandle(t, m, Request{Action: "connect_cancel", FlowID: start.ID})
		return u.Query().Get("client_id")
	}
	first := client()
	if client() != first || f.clients != 1 {
		t.Fatalf("registered %d clients; a daemon keeps its own", f.clients)
	}
	// Another daemon on this computer has its own port, so its own client.
	m.SetLoopbackPort(awsTestPort + 1)
	if other := client(); other == first || f.clients != 2 {
		t.Fatal("two ports share a client")
	}
	m.SetLoopbackPort(awsTestPort)
	// Providers expire a client after 90 days; replace it long before.
	old, _ := json.Marshal(oauthRegistration{RedirectURL: awsRedirect, ClientID: first, RegisteredAt: time.Now().Add(-81 * 24 * time.Hour)})
	_ = m.vault.Set("oauth-registration:aws", string(old))
	if client() == first || f.clients != 3 {
		t.Fatal("an old registration was kept")
	}
	// A client the provider no longer knows is forgotten at the token step.
	start := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
	state, code := f.grant(start.AuthorizationURL)
	f.tokenError = "invalid_client"
	got := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID, Callback: awsRedirect + "?code=" + code + "&state=" + state}).Flow
	if got.Status != "failed" {
		t.Fatalf("flow %+v", got)
	}
	if _, ok, _ := m.vault.Get("oauth-registration:aws"); ok {
		t.Fatal("the rejected client was kept")
	}
}

// ---- accounts that signed in with the CLI -------------------------------

func legacyAWS(m *Manager, id, name, arn string) {
	m.records[id] = &record{
		Account: Account{ID: id, Integration: "aws", Name: name, Identity: arn, AuthMethod: "aws_profile", Region: "us-east-1", Enabled: true, Status: "connected",
			AWS: &AWSDetails{AccountID: "113732353381", AccountName: "Blank Canvas", Who: "AccountFullAccessRole", CheckedAt: time.Now()}, Tools: awsTools(), History: []Event{}},
		Grants: map[string]string{},
	}
	for _, tl := range awsTools() {
		if tl.Name == "read" || tl.Name == "execute" || tl.Name == "whoami" {
			m.records[id].Grants[tl.Name] = fingerprint(tl)
		}
	}
}

func TestAWSLegacyAccountsSignInAgainInPlace(t *testing.T) {
	f := newAWSFixture(t)
	m := f.manager()
	// mewla-2 and mewla-3 were two profiles of one sign-in.
	legacyAWS(m, "aws-2", "Blank Canvas (mewla-2)", awsTestARN)
	legacyAWS(m, "aws-3", "Blank Canvas (mewla-3)", awsTestARN)
	legacyAWS(m, "aws-other", "Elsewhere", "arn:aws:sts::999999999999:assumed-role/Other/me")
	m.records["aws-other"].Account.AWS.AccountID = "999999999999"
	m.markLegacyAWS()
	for id, r := range m.records {
		if r.Account.Status != "authorization_required" {
			t.Fatalf("%s reads %s", id, r.Account.Status)
		}
	}
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: "aws-3", Tool: "whoami", Arguments: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "Sign in again") {
		t.Fatalf("a CLI account ran: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("a CLI account reached AWS")
	}

	a := f.signIn(m, &Input{Integration: "aws", AccountID: "aws-3"})
	if a.ID != "aws-3" || a.Name != "Blank Canvas (mewla-3)" || a.Status != "connected" {
		t.Fatalf("renewed %+v", a)
	}
	if m.records["aws-3"].Grants["execute"] == "" || m.records["aws-3"].Grants["read"] == "" {
		t.Fatal("the renewal lost what the person allowed")
	}
	if m.records["aws-2"].Account.Status != "authorization_required" || len(m.records) != 3 {
		t.Fatal("the other card changed")
	}
	if r := m.records["aws-3"]; r.OAuth == nil || r.Account.AuthMethod != "oauth" {
		t.Fatal("the renewed account still has the CLI sign-in")
	}
	// The other card of the same sign-in renews on its own turn.
	if b := f.signIn(m, &Input{Integration: "aws", AccountID: "aws-2"}); b.ID != "aws-2" {
		t.Fatalf("renewed %s", b.ID)
	}
	// Another AWS account signing in from a card is a new account, not that card.
	f.arn = "arn:aws:sts::113732353381:assumed-role/AccountFullAccessRole/54784408-0061-700a-cbe9-387d91b9b747"
	c := f.signIn(m, &Input{Integration: "aws", AccountID: "aws-other"})
	if c.ID == "aws-other" || m.records["aws-other"].Account.Status != "authorization_required" {
		t.Fatalf("a different AWS account replaced the card: %s", c.ID)
	}
}

// ---- calls --------------------------------------------------------------

func connectedAWS(t *testing.T) (*awsFixture, *Manager, *Account) {
	f := newAWSFixture(t)
	m := f.manager()
	a := f.signIn(m, nil)
	return f, m, a
}

func invokeAWS(m *Manager, a *Account, tool string, args string) (map[string]any, error) {
	r, err := m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: tool, Arguments: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if jerr := json.Unmarshal(r.Result, &out); jerr != nil {
		return nil, jerr
	}
	return out, nil
}

func TestAWSWhoamiAndRead(t *testing.T) {
	f, m, a := connectedAWS(t)
	who, err := invokeAWS(m, a, "whoami", `{}`)
	if err != nil || who["account"] != "113732353381" || who["arn"] != awsTestARN || who["region"] != "us-east-1" {
		t.Fatalf("whoami %v %v", who, err)
	}
	out, err := invokeAWS(m, a, "read", `{"service":"ec2","operation":"DescribeInstances","params":{"Filters":[{"Name":"tag:Name","Values":["web"]}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	call := f.lastCall()
	if call.Service != "ec2" || call.Operation != "DescribeInstances" || call.Region != "us-east-1" || call.Params["Filters"] == nil {
		t.Fatalf("call %+v", call)
	}
	if out["account"] != "113732353381" || out["region"] != "us-east-1" {
		t.Fatalf("reply %v", out)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), "i-123") || strings.Contains(string(raw), "c2VjcmV0") || !strings.Contains(string(raw), awsRedacted) {
		t.Fatalf("user data was not redacted: %s", raw)
	}
	// Bedrock's model list is an ordinary read, in a region the person names.
	out, err = invokeAWS(m, a, "read", `{"service":"bedrock","operation":"list-foundation-models","region":"us-west-2"}`)
	if err != nil || f.lastCall().Operation != "ListFoundationModels" || f.lastCall().Region != "us-west-2" {
		t.Fatalf("bedrock %v %+v", err, f.lastCall())
	}
	if !strings.Contains(string(mustJSON(out)), "anthropic.claude") {
		t.Fatalf("models %v", out)
	}
	// The CLI's name for a service works.
	if _, err = invokeAWS(m, a, "read", `{"service":"s3api","operation":"ListBuckets"}`); err != nil && !strings.Contains(err.Error(), "unknown operation s3/ListBuckets") {
		t.Fatal(err)
	}
	if f.lastCall().Service != "s3" {
		t.Fatalf("s3api reached %s", f.lastCall().Service)
	}
	if got := m.records[a.ID].Account.Status; got != "connected" {
		t.Fatalf("status %s", got)
	}
}

func TestAWSReadRefusesWhatChanges(t *testing.T) {
	f, m, a := connectedAWS(t)
	before := len(f.calls)
	for _, args := range []string{
		`{"service":"ec2","operation":"RunInstances"}`,
		`{"service":"rds","operation":"DeleteDBInstance"}`,
		`{"service":"secretsmanager","operation":"GetSecretValue"}`,
		`{"service":"sts","operation":"GetSessionToken"}`,
		`{"service":"ssm","operation":"GetParameter","params":{"Name":"x","WithDecryption":true}}`,
		`{"service":"ec2","operation":"run-instances"}`,
		`{"service":"ec2","operation":"describeInstances; import os"}`,
		`{"service":"EC2","operation":"DescribeInstances"}`,
		`{"service":"ec2","operation":"DescribeInstances","region":"nowhere"}`,
		`{"service":"ec2","operation":"DescribeInstances","params":"x"}`,
		`{"operation":"DescribeInstances"}`,
		`{"service":"ec2"}`,
	} {
		if _, err := invokeAWS(m, a, "read", args); err == nil {
			t.Errorf("read ran %s", args)
		}
	}
	if len(f.calls) != before {
		t.Fatal("a refused call reached AWS")
	}
	if got := m.records[a.ID].Account.Status; got != "connected" {
		t.Fatalf("a refusal marked the account %s", got)
	}
}

func TestAWSExecuteNeedsTheGrant(t *testing.T) {
	f, m, a := connectedAWS(t)
	args := `{"service":"ec2","operation":"RunInstances","params":{"ImageId":"ami-1"}}`
	if _, err := invokeAWS(m, a, "execute", args); err == nil {
		t.Fatal("execute ran without a grant")
	}
	mustHandle(t, m, Request{Action: "policy", ID: a.ID, Tool: "execute", Allowed: true})
	if _, err := invokeAWS(m, a, "execute", args); err != nil {
		t.Fatal(err)
	}
	if call := f.lastCall(); call.Operation != "RunInstances" || call.Params["ImageId"] != "ami-1" {
		t.Fatalf("call %+v", call)
	}
	// kebab-case names come out as the API's PascalCase.
	if _, err := invokeAWS(m, a, "execute", `{"service":"ec2","operation":"run-instances"}`); err != nil || f.lastCall().Operation != "RunInstances" {
		t.Fatalf("%v %+v", err, f.lastCall())
	}
}

func TestAWSScriptCarriesNoModelText(t *testing.T) {
	hostile := []string{
		`"""`, `'''`, `\`, `\\n`, "line\nbreak", "tab\tnul\x00", `x" + __import__("os").system("id") + "`, " ", "emoji 😀 and \U0001F9EA",
		`"); import os; os.system("id"); ("`, `{{.}}`, `%s`, "\x1b[31m", "'; DROP",
	}
	for _, text := range hostile {
		params := map[string]any{"Name": text, "Nested": map[string]any{"List": []any{text, 1, true, nil}}, text: "key"}
		code, err := awsScript("ec2", "DescribeInstances", "us-east-1", params)
		if err != nil {
			t.Fatal(err)
		}
		// The first line is the only place values appear; the rest is fixed.
		lines := strings.Split(code, "\n")
		fixed := strings.Join(lines[1:], "\n")
		for _, part := range []string{text, "ec2", "DescribeInstances"} {
			if len(part) > 2 && strings.Contains(fixed, part) {
				t.Errorf("%q leaked outside the data line", part)
			}
		}
		back := decodeAWSScript(t, code)
		if back.Service != "ec2" || back.Operation != "DescribeInstances" || back.Region != "us-east-1" || back.Params["Name"] != text {
			t.Errorf("%q came back as %+v", text, back)
		}
		if back.Params[text] != "key" || back.Params["Nested"].(map[string]any)["List"].([]any)[0] != text {
			t.Errorf("%q: nested values changed", text)
		}
		if strings.Count(lines[0], "\n") != 0 || strings.Contains(lines[0], "\x00") {
			t.Errorf("%q: the data line is not one clean line", text)
		}
	}
	// The whole script is a fixed shape.
	code, _ := awsScript("sts", "GetCallerIdentity", "us-east-1", map[string]any{})
	for _, want := range []string{"await call_boto3(", "except Exception as e:", "str(e)[:1500]", "\nresult\n"} {
		if !strings.Contains(code, want) {
			t.Errorf("script lacks %q", want)
		}
	}
}

func TestAWSFailuresAreTold(t *testing.T) {
	f, m, a := connectedAWS(t)
	// An organization's policy denying a call is the service's answer: the
	// account stays signed in.
	_, err := invokeAWS(m, a, "read", `{"service":"ec2","operation":"DescribeRegions","region":"eu-west-1"}`)
	if err == nil || !strings.Contains(err.Error(), "service control policy") || strings.Contains(err.Error(), "Sign in again") {
		t.Fatalf("error %v", err)
	}
	if got := m.records[a.ID].Account.Status; got != "connected" {
		t.Fatalf("a denial marked the account %s", got)
	}
	// AWS refusing the sign-in needs a new one.
	f.mu.Lock()
	f.access += "-revoked"
	f.mu.Unlock()
	_, err = invokeAWS(m, a, "read", `{"service":"ec2","operation":"DescribeInstances"}`)
	if err == nil || err.Error() != "AWS session expired. Sign in again." || m.records[a.ID].Account.Status != "authorization_required" {
		t.Fatalf("error %v, status %s", err, m.records[a.ID].Account.Status)
	}
}

func TestAWSExpiredSessionAsksToSignInAgain(t *testing.T) {
	f, m, a := connectedAWS(t)
	f.ops["ec2/DescribeVpcs"] = func(map[string]any) (any, string) {
		return nil, "An error occurred (ExpiredToken) when calling the DescribeVpcs operation: The security token included in the request is expired"
	}
	_, err := invokeAWS(m, a, "read", `{"service":"ec2","operation":"DescribeVpcs"}`)
	if err == nil || err.Error() != "AWS session expired. Sign in again." {
		t.Fatalf("error %v", err)
	}
	if got := m.records[a.ID].Account.Status; got != "authorization_required" {
		t.Fatalf("status %s", got)
	}

	// After 12 hours AWS refuses the refresh itself.
	f2, m2, a2 := connectedAWS(t)
	f2.tokenError = "invalid_grant"
	m2.records[a2.ID].Account.Status = "connected"
	secretKey := "integration:" + a2.ID
	raw, _, _ := m2.vault.Get(secretKey)
	var secret oauthSecret
	_ = json.Unmarshal([]byte(raw), &secret)
	secret.Token.Expiry = time.Now().Add(-time.Minute)
	encoded, _ := json.Marshal(secret)
	_ = m2.vault.Set(secretKey, string(encoded))
	if _, err = invokeAWS(m2, a2, "whoami", `{}`); err == nil || !strings.Contains(err.Error(), "authorization") && !strings.Contains(err.Error(), "sign") && !strings.Contains(err.Error(), "Sign") {
		t.Fatalf("error %v", err)
	}
	if got := m2.records[a2.ID].Account.Status; got != "authorization_required" {
		t.Fatalf("status %s", got)
	}
}

func TestAWSScriptErrorAndTasks(t *testing.T) {
	f, m, a := connectedAWS(t)
	f.mu.Lock()
	f.scriptError = "Code validation failed: Blocked attribute access"
	f.mu.Unlock()
	if _, err := invokeAWS(m, a, "read", `{"service":"ec2","operation":"DescribeInstances"}`); err == nil || !strings.Contains(err.Error(), "Code validation failed") {
		t.Fatalf("error %v", err)
	}
	f.mu.Lock()
	f.scriptError = ""
	f.working = true
	f.mu.Unlock()
	out, err := invokeAWS(m, a, "read", `{"service":"ec2","operation":"DescribeInstances"}`)
	if err != nil || f.taskCalls != 1 || !strings.Contains(string(mustJSON(out)), "Done") {
		t.Fatalf("task %v %v (polled %d times)", out, err, f.taskCalls)
	}
}

func TestAWSDocumentation(t *testing.T) {
	_, m, a := connectedAWS(t)
	found, err := invokeAWS(m, a, "search", `{"query":"rds describe instances API","limit":2}`)
	if err != nil || !strings.Contains(string(mustJSON(found)), "docs.aws.amazon.com/rds/x") {
		t.Fatalf("search %v %v", found, err)
	}
	page, err := invokeAWS(m, a, "docs", `{"url":"https://docs.aws.amazon.com/rds/x"}`)
	if err != nil || !strings.Contains(string(mustJSON(page)), "DescribeDBInstances") {
		t.Fatalf("docs %v %v", page, err)
	}
	for _, bad := range []string{"http://docs.aws.amazon.com/x", "https://example.com/x", "https://docs.aws.amazon.com.evil.test/x", "file:///etc/passwd", "https://169.254.169.254/"} {
		if _, err := invokeAWS(m, a, "docs", `{"url":"`+bad+`"}`); err == nil {
			t.Errorf("docs read %s", bad)
		}
	}
}

// ---- tokens -------------------------------------------------------------

func expireAccess(t *testing.T, m *Manager, id string) {
	t.Helper()
	key := "integration:" + id
	raw, _, _ := m.vault.Get(key)
	var secret oauthSecret
	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		t.Fatal(err)
	}
	secret.Token.Expiry = time.Now().Add(-time.Minute)
	encoded, _ := json.Marshal(secret)
	_ = m.vault.Set(key, string(encoded))
}

func TestAWSRefreshRotatesOnceAndIsKept(t *testing.T) {
	f, m, a := connectedAWS(t)
	first := f.refresh
	expireAccess(t, m, a.ID)
	f.refreshDelay = 50 * time.Millisecond
	var wg sync.WaitGroup
	var failed atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := invokeAWS(m, a, "whoami", `{}`); err != nil {
				failed.Add(1)
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.refreshes != 1 || failed.Load() != 0 {
		t.Fatalf("%d refreshes for four calls; a refresh token works once", f.refreshes)
	}
	raw, _, _ := m.vault.Get("integration:" + a.ID)
	var secret oauthSecret
	_ = json.Unmarshal([]byte(raw), &secret)
	if secret.Token.RefreshToken == first || secret.Token.RefreshToken != f.refresh || secret.Token.AccessToken != f.access {
		t.Fatal("the rotated tokens were not kept")
	}
	// The next hour uses the kept token with no refresh.
	if _, err := invokeAWS(m, a, "whoami", `{}`); err != nil || f.refreshes != 1 {
		t.Fatalf("%v, %d refreshes", err, f.refreshes)
	}
}

func TestAWSSessionsSurviveARestart(t *testing.T) {
	f := newAWSFixture(t)
	m := f.manager()
	a := f.signIn(m, nil)
	expireAccess(t, m, a.ID)
	if _, err := invokeAWS(m, a, "whoami", `{}`); err != nil {
		t.Fatal(err)
	}
	again, err := newManager(m.path, m.vault, m.http)
	if err != nil {
		t.Fatal(err)
	}
	again.SetLoopbackPort(awsTestPort)
	if _, err := invokeAWS(again, a, "whoami", `{}`); err != nil {
		t.Fatal(err)
	}
	if again.records[a.ID].Account.Status != "connected" {
		t.Fatal("a restart signed the account out")
	}
}

func TestAWSOpeningTheCardRefreshesItsDetails(t *testing.T) {
	f, m, a := connectedAWS(t)
	r := m.records[a.ID]
	r.Account.AWS.CheckedAt = time.Now().Add(-7 * time.Hour)
	f.account = "Renamed"
	got := mustHandle(t, m, Request{Action: "get", ID: a.ID}).Account
	if got.AWS.AccountName != "Renamed" || got.Name != "Blank Canvas" && got.Name != "Renamed" {
		t.Fatalf("details %+v name %s", got.AWS, got.Name)
	}
	// A denied name leaves the AWS account ID as the name of a new account.
	f2 := newAWSFixture(t)
	f2.account = ""
	m2 := f2.manager()
	b := f2.signIn(m2, nil)
	if b.Name != "AWS 113732353381" || b.AWS.AccountName != "" {
		t.Fatalf("account %+v", b)
	}
}

func TestAWSDisconnectKeepsNothing(t *testing.T) {
	_, m, a := connectedAWS(t)
	mustHandle(t, m, Request{Action: "disconnect", ID: a.ID})
	if _, ok, _ := m.vault.Get("integration:" + a.ID); ok {
		t.Fatal("the grant stayed in the vault")
	}
	if got := m.records[a.ID].Account.Status; got != "disconnected" {
		t.Fatalf("status %s", got)
	}
}

func TestAWSKind(t *testing.T) {
	for name, want := range map[string]string{
		"DescribeInstances": "read", "ListBuckets": "read", "GetMetricData": "read", "BatchGetItem": "read", "HeadBucket": "read",
		"LookupEvents": "read", "SearchTransitGatewayRoutes": "read", "GetAccountPasswordPolicy": "read", "GetChangeToken": "read",
		"RunInstances": "write", "DeleteDBInstance": "write", "PutObject": "write", "StartQuery": "write", "TerminateInstances": "write",
		"Invoke": "write", "SendMessage": "write", "Listen": "write", "Getaway": "write", "Describer": "write",
		"GetSecretValue": "write", "BatchGetSecretValue": "write", "GetSessionToken": "write", "GetFederationToken": "write",
		"GetAuthorizationToken": "write", "GetPasswordData": "write", "GetRoleCredentials": "write", "GetClusterCredentials": "write",
		"GetCredentialsForIdentity": "write", "GetOpenIdToken": "write", "GetDownloadUrlForLayer": "write",
	} {
		if got := awsKind(name, nil); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
	if awsKind("GetParameter", nil) != "read" || awsKind("GetParameter", map[string]any{"WithDecryption": false}) != "read" {
		t.Error("an undecrypted parameter is a read")
	}
	for _, op := range []string{"GetParameter", "GetParameters", "GetParametersByPath"} {
		if awsKind(op, map[string]any{"WithDecryption": true}) != "write" {
			t.Errorf("%s with decryption returns secrets", op)
		}
	}
}

func TestAWSRedact(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"Credentials":{"AccessKeyId":"ASIA1","SecretAccessKey":"s3cret","SessionToken":"tok"},
		"Function":{"Environment":{"Variables":{"DB_PASSWORD":"hunter2","REGION":"us-east-1"}}},
		"TaskDefinition":{"containerDefinitions":[{"environment":[{"name":"API_KEY","value":"abc"},{"name":"MODE","value":"prod"}]}]},
		"SecretString":"x","Instances":[{"UserData":{"Value":"IyEvYmluL3No"},"Id":"i-1"}]}`), &v)
	out, _ := json.Marshal(awsRedact(v))
	text := string(out)
	for _, leaked := range []string{"s3cret", "tok\"", "hunter2", "abc", "IyEvYmluL3No", "\"x\""} {
		if strings.Contains(text, leaked) {
			t.Errorf("%q leaked: %s", leaked, text)
		}
	}
	for _, kept := range []string{"ASIA1", "us-east-1", "prod", "i-1"} {
		if !strings.Contains(text, kept) {
			t.Errorf("%q was removed: %s", kept, text)
		}
	}
}

func TestParseAWSPrincipal(t *testing.T) {
	for arn, want := range map[string][2]string{
		"arn:aws:iam::725098487500:root":                                              {"Root user", ""},
		"arn:aws:sts::113732353381:assumed-role/AccountFullAccessRole/54784408-0061":  {"AccountFullAccessRole", "54784408-0061"},
		"arn:aws:iam::123456789012:user/audit":                                        {"IAM user audit", ""},
		"arn:aws:iam::123456789012:user/team/audit":                                   {"IAM user audit", ""},
		"arn:aws:sts::1:assumed-role/AWSReservedSSO_Admin_0123456789abcdef/ann@x.com": {"Admin", "ann@x.com"},
		"arn:aws:sts::1:federated-user/bob":                                           {"Federated user bob", ""},
		"arn:aws:iam::1:role/ops/deploy":                                              {"deploy", ""},
		"nonsense":                                                                    {"AWS identity", ""},
		"arn:aws:iam::1:something":                                                    {"AWS identity", ""},
	} {
		if who, session := parseAWSPrincipal(arn); who != want[0] || session != want[1] {
			t.Errorf("%s: got %q, %q; want %q, %q", arn, who, session, want[0], want[1])
		}
		if who, _ := parseAWSPrincipal(arn); strings.HasPrefix(who, "arn:") {
			t.Errorf("%s: the headline is an ARN", arn)
		}
	}
}
