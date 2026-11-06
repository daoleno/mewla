package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const tsTestSecret = "tskey-client-kTestID1CNTRL-s3cretValue9"
const tsTestStored = "kTestID1CNTRL:" + tsTestSecret

// tailscaleFake answers the parts of Tailscale's API the connection uses.
type tailscaleFake struct {
	mu       sync.Mutex
	tokens   int
	seen     []string
	deleted  []string
	scope    string
	badLogin bool
	devices  string
}

func (f *tailscaleFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v2/oauth/token":
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		if f.badLogin || form.Get("client_id") != "kTestID1CNTRL" || form.Get("client_secret") != tsTestSecret || form.Get("grant_type") != "client_credentials" {
			return reply(400, `{"error":"invalid_client"}`), nil
		}
		f.tokens++
		return reply(200, `{"access_token":"tskey-api-minted`+string(rune('0'+f.tokens))+`","token_type":"Bearer","expires_in":3600,"scope":"`+f.scope+`"}`), nil
	case r.URL.Path == "/api/v2" && r.URL.Query().Get("outputOpenapiSchema") == "true":
		raw, err := os.ReadFile("testdata/tailscale_openapi.yaml")
		if err != nil {
			return reply(500, ""), nil
		}
		return reply(200, string(raw)), nil
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tskey-api-minted") {
		return reply(401, `{"message":"unauthorized"}`), nil
	}
	f.seen = append(f.seen, r.Method+" "+r.URL.RequestURI())
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v2/tailnet/-/devices":
		return reply(200, f.devices), nil
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/v2/device/"):
		f.deleted = append(f.deleted, strings.TrimPrefix(r.URL.Path, "/api/v2/device/"))
		return reply(200, ``), nil
	}
	return reply(404, `{"message":"not found"}`), nil
}

func newTailscaleFake() *tailscaleFake {
	return &tailscaleFake{scope: "all:read devices:core", devices: `{"devices":[{"id":"1","name":"laptop.tail1234.ts.net","hostname":"laptop"}]}`}
}

func connectTailscale(t *testing.T) (*Manager, *tailscaleFake, Account) {
	t.Helper()
	fake := newTailscaleFake()
	m := testManager(t, fake.RoundTrip)
	r, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "tailscale", Name: "ignored", Credential: tsTestStored}})
	if err != nil {
		t.Fatal(err)
	}
	return m, fake, *r.Account
}

func invokeTS(m *Manager, id, tool string, args any) (Response, error) {
	raw, _ := json.Marshal(args)
	return m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: tool, Arguments: raw})
}

func TestTailscaleConnectNamesTheTailnetAndGrantsReads(t *testing.T) {
	m, _, a := connectTailscale(t)
	if a.Identity != "tail1234.ts.net" || a.Name != "tail1234.ts.net" || a.Status != "connected" {
		t.Fatalf("account %+v", a)
	}
	if a.Access.Read != "allowed" || a.Access.Write != "off" {
		t.Fatalf("access %+v", a.Access)
	}
	if strings.Join(a.Scopes, " ") != "all:read devices:core" {
		t.Fatalf("scopes %v", a.Scopes)
	}
	raw, _ := os.ReadFile(m.path)
	if strings.Contains(string(raw), tsTestSecret) || strings.Contains(string(raw), "minted") {
		t.Fatal("the catalog file holds a secret")
	}
	if got, ok, _ := m.vault.Get("integration:" + a.ID); !ok || got != tsTestStored {
		t.Fatalf("vault %q", got)
	}
}

func TestTailscaleAcceptsTheSecretAloneAndRejectsOthers(t *testing.T) {
	fake := newTailscaleFake()
	m := testManager(t, fake.RoundTrip)
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "tailscale", Name: "x", Credential: tsTestSecret}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "hunter2", "a b c", "tskey-auth-abc-def"} {
		if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "tailscale", Name: "x", Credential: bad}}); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
	fake.badLogin = true
	_, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "tailscale", Name: "x", Credential: "other:tskey-client-other-zzzz"}})
	if err == nil || strings.Contains(err.Error(), "zzzz") {
		t.Fatalf("error %v", err)
	}
}

func TestTailscaleConnectingTheSameTailnetAgainReplacesTheCredential(t *testing.T) {
	m, _, first := connectTailscale(t)
	if _, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "tailscale", Name: "x", Credential: tsTestStored}}); err != nil {
		t.Fatal(err)
	}
	live := 0
	for _, r := range m.records {
		if r.Account.Integration == "tailscale" {
			live++
			if r.Account.ID != first.ID {
				t.Fatal("a second account was made")
			}
		}
	}
	if live != 1 {
		t.Fatalf("%d accounts", live)
	}
}

func TestTailscaleSearchAndRead(t *testing.T) {
	m, fake, a := connectTailscale(t)
	out, err := invokeTS(m, a.ID, "search", map[string]any{"query": "delete device"})
	if err != nil {
		t.Fatal(err)
	}
	var found struct {
		Operations []map[string]any `json:"operations"`
	}
	if json.Unmarshal(out.Result, &found) != nil || len(found.Operations) == 0 || found.Operations[0]["operation"] != "deleteDevice" {
		t.Fatalf("search %s", out.Result)
	}
	if found.Operations[0]["scope"] != "devices:core" || found.Operations[0]["read"] != false {
		t.Fatalf("operation %v", found.Operations[0])
	}
	out, err = invokeTS(m, a.ID, "search", map[string]any{"query": "setDeviceName"})
	if err != nil || !strings.Contains(string(out.Result), `"body_required":true`) || !strings.Contains(string(out.Result), "The new device name") {
		t.Fatalf("detail %s %v", out.Result, err)
	}
	if _, err = invokeTS(m, a.ID, "read", map[string]any{"operation": "listTailnetDevices", "query": map[string]any{"fields": "all"}}); err != nil {
		t.Fatal(err)
	}
	last := fake.seen[len(fake.seen)-1]
	if last != "GET /api/v2/tailnet/-/devices?fields=all" {
		t.Fatalf("request %s", last)
	}
	for _, bad := range []map[string]any{
		{"operation": "deleteDevice", "path": map[string]any{"deviceId": "1"}}, // a change through read
		{"operation": "nope"},      // not in the reference
		{"operation": "getDevice"}, // missing path parameter
		{"operation": "listTailnetDevices", "path": map[string]any{"tailnet": "a/b"}}, // not a single segment
		{"operation": "listTailnetDevices", "query": map[string]any{"bogus": "1"}},
	} {
		if _, err := invokeTS(m, a.ID, "read", bad); err == nil {
			t.Fatalf("%v was allowed", bad)
		}
	}
}

func TestTailscaleExecuteNeedsAllowedChangesAndAuthorization(t *testing.T) {
	m, fake, a := connectTailscale(t)
	del := map[string]any{"operation": "deleteDevice", "path": map[string]any{"deviceId": "nodeABC"}, "authorize": true}
	if _, err := invokeTS(m, a.ID, "execute", del); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("a change ran before changes were allowed: %v", err)
	}
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "write", Allowed: true})
	noAuth := map[string]any{"operation": "deleteDevice", "path": map[string]any{"deviceId": "nodeABC"}}
	if _, err := invokeTS(m, a.ID, "execute", noAuth); err == nil {
		t.Fatal("a change ran without authorization")
	}
	if len(fake.deleted) != 0 {
		t.Fatal("deleted early")
	}
	if _, err := invokeTS(m, a.ID, "execute", del); err != nil {
		t.Fatal(err)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "nodeABC" {
		t.Fatalf("deleted %v", fake.deleted)
	}
	body := map[string]any{"operation": "setDeviceName", "path": map[string]any{"deviceId": "1"}, "authorize": true}
	if _, err := invokeTS(m, a.ID, "execute", body); err == nil {
		t.Fatal("a required body was left out")
	}
}

func TestTailscaleReusesOneAccessTokenAndRedactsSecrets(t *testing.T) {
	m, fake, a := connectTailscale(t)
	fake.devices = `{"devices":[{"name":"x.tail1234.ts.net","note":"` + tsTestSecret + `"}]}`
	before := fake.tokens
	for i := 0; i < 3; i++ {
		out, err := invokeTS(m, a.ID, "read", map[string]any{"operation": "listTailnetDevices"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out.Result), tsTestSecret) || !strings.Contains(string(out.Result), "[REDACTED]") {
			t.Fatalf("result %s", out.Result)
		}
	}
	if fake.tokens != before {
		t.Fatalf("minted %d tokens for three calls", fake.tokens-before)
	}
}

func TestTailscaleSpecIsCachedAndUsedWhenTailscaleIsUnreachable(t *testing.T) {
	m, fake, a := connectTailscale(t)
	if _, err := invokeTS(m, a.ID, "search", map[string]any{"query": "dns"}); err != nil {
		t.Fatal(err)
	}
	m.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v2" {
			return reply(503, ""), nil
		}
		return fake.RoundTrip(r)
	})}
	tailscaleSpecs.Lock()
	tailscaleSpecs.byFile = map[string]*tsCached{}
	tailscaleSpecs.Unlock()
	out, err := invokeTS(m, a.ID, "search", map[string]any{"query": "nameservers"})
	if err != nil || !strings.Contains(string(out.Result), "listDnsNameservers") {
		t.Fatalf("cached copy not used: %s %v", out.Result, err)
	}
}

func TestTailscaleRevokedClientNeedsReconnect(t *testing.T) {
	m, fake, a := connectTailscale(t)
	tailscaleTokens.Lock()
	tailscaleTokens.byKey = map[string]tsToken{}
	tailscaleTokens.Unlock()
	fake.badLogin = true
	if _, err := invokeTS(m, a.ID, "read", map[string]any{"operation": "listTailnetDevices"}); err == nil {
		t.Fatal("a revoked client worked")
	}
	if got := mustHandle(t, m, Request{Action: "get", ID: a.ID}).Account.Status; got != "authorization_required" {
		t.Fatalf("status %s", got)
	}
}

type fakeTSBrowser struct {
	ready  bool
	client TailscaleClient
	err    error
	gate   chan struct{}
	calls  int
}

func (b *fakeTSBrowser) Ready() bool { return b.ready }
func (b *fakeTSBrowser) Create(ctx context.Context) (TailscaleClient, error) {
	b.calls++
	if b.gate != nil {
		select {
		case <-b.gate:
		case <-ctx.Done():
			return TailscaleClient{}, ctx.Err()
		}
	}
	return b.client, b.err
}

func startTS(t *testing.T, m *Manager) ConnectFlow {
	t.Helper()
	r := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "tailscale"}})
	return *r.Flow
}

func settleTS(t *testing.T, m *Manager, id string) Response {
	t.Helper()
	for i := 0; i < 100; i++ {
		r := mustHandle(t, m, Request{Action: "connect_status", FlowID: id})
		if r.Flow.Status != "waiting" || r.Flow.CodeRequired {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("flow did not settle")
	return Response{}
}

func TestTailscaleConnectThroughTheBrowser(t *testing.T) {
	fake := newTailscaleFake()
	m := testManager(t, fake.RoundTrip)
	browser := &fakeTSBrowser{ready: true, client: TailscaleClient{ID: "kTestID1CNTRL", Secret: tsTestSecret}}
	m.SetTailscaleBrowser(browser)
	flow := startTS(t, m)
	if flow.AuthorizationURL != "" || flow.CodeRequired {
		t.Fatalf("the browser path sent the person to a page: %+v", flow)
	}
	r := settleTS(t, m, flow.ID)
	if r.Flow.Status != "connected" || r.Account == nil || r.Account.Access.Read != "allowed" || r.Account.Access.Write != "off" {
		t.Fatalf("flow %+v account %+v", r.Flow, r.Account)
	}
	if got, _, _ := m.vault.Get("integration:" + r.Account.ID); got != tsTestStored {
		t.Fatal("the credential was not stored")
	}
}

func TestTailscaleBrowserFallsBackToPasting(t *testing.T) {
	fake := newTailscaleFake()
	m := testManager(t, fake.RoundTrip)
	m.SetTailscaleBrowser(&fakeTSBrowser{ready: true, err: &TailscaleBrowserError{"Chrome isn't signed in to Tailscale."}})
	flow := startTS(t, m)
	r := settleTS(t, m, flow.ID)
	if r.Flow.Status != "waiting" || !r.Flow.CodeRequired || r.Flow.AuthorizationURL != TailscaleCredentialsURL || !strings.Contains(r.Flow.Message, "isn't signed in") {
		t.Fatalf("flow %+v", r.Flow)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: flow.ID, Callback: "nonsense"}); err == nil {
		t.Fatal("nonsense was accepted")
	}
	fin := mustHandle(t, m, Request{Action: "connect_finish", FlowID: flow.ID, Callback: "kTestID1CNTRL " + tsTestSecret})
	if fin.Flow.Status != "connected" || fin.Account == nil {
		t.Fatalf("flow %+v", fin.Flow)
	}
}

func TestTailscalePasteWithoutABrowser(t *testing.T) {
	fake := newTailscaleFake()
	m := testManager(t, fake.RoundTrip)
	m.SetTailscaleBrowser(&fakeTSBrowser{ready: false})
	flow := startTS(t, m)
	if !flow.CodeRequired || flow.AuthorizationURL != TailscaleCredentialsURL {
		t.Fatalf("flow %+v", flow)
	}
	fin := mustHandle(t, m, Request{Action: "connect_finish", FlowID: flow.ID, Callback: tsTestSecret})
	if fin.Flow.Status != "connected" {
		t.Fatalf("flow %+v", fin.Flow)
	}
}

func TestTailscaleOneBrowserJobAtATimeAndCancel(t *testing.T) {
	fake := newTailscaleFake()
	m := testManager(t, fake.RoundTrip)
	browser := &fakeTSBrowser{ready: true, gate: make(chan struct{})}
	m.SetTailscaleBrowser(browser)
	first := startTS(t, m)
	second := startTS(t, m)
	if !second.CodeRequired {
		t.Fatal("a second browser job started")
	}
	cancelled := mustHandle(t, m, Request{Action: "connect_cancel", FlowID: first.ID})
	if cancelled.Flow.Status != "cancelled" {
		t.Fatalf("flow %+v", cancelled.Flow)
	}
	for i := 0; i < 100; i++ {
		m.mu.Lock()
		jobs := m.tailscaleJobs
		m.mu.Unlock()
		if jobs == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, r := range m.records {
		if r.Account.Integration == "tailscale" {
			t.Fatal("a cancelled connect made an account")
		}
	}
}

func scriptReply(o map[string]any) func(context.Context, string) (string, error) {
	return func(_ context.Context, script string) (string, error) {
		raw, _ := json.Marshal(o)
		return "### Result\n\"MEWLA:" + base64.StdEncoding.EncodeToString(raw) + "\"\n", nil
	}
}

func TestTailscaleConsoleResults(t *testing.T) {
	cases := []struct {
		reply map[string]any
		ok    bool
		said  string
	}{
		{map[string]any{"state": "created", "id": "kAbc1CNTRL", "secret": "tskey-client-kAbc1CNTRL-Zz9"}, true, ""},
		{map[string]any{"state": "created", "id": "kAbc1CNTRL", "secret": "tskey-client-kOther-Zz9"}, false, "could not read"},
		{map[string]any{"state": "signed_out"}, false, "isn't signed in"},
		{map[string]any{"state": "needs_you"}, false, "only you"},
		{map[string]any{"state": "needs_tag"}, false, "tags"},
		{map[string]any{"state": "unrecognised", "step": "x"}, false, "recognise"},
		{map[string]any{"state": "created_unread"}, false, "could not read"},
	}
	for _, c := range cases {
		b := &chromeTools{run: scriptReply(c.reply)}
		client, err := b.Create(context.Background())
		if c.ok != (err == nil) || c.ok && client.ID != "kAbc1CNTRL" {
			t.Fatalf("%v: %v %v", c.reply, client, err)
		}
		if !c.ok && !strings.Contains(err.Error(), c.said) {
			t.Fatalf("%v: %v", c.reply, err)
		}
	}
	b := &chromeTools{run: func(context.Context, string) (string, error) { return "", io.EOF }}
	if _, err := b.Create(context.Background()); err == nil {
		t.Fatal("a browser failure was swallowed")
	}
}

func TestTailscaleConsoleScriptCarriesItsConfigAndNeverTypesSecrets(t *testing.T) {
	var seen string
	b := &chromeTools{run: func(_ context.Context, script string) (string, error) {
		seen = script
		return scriptReply(map[string]any{"state": "dry_run"})(context.Background(), script)
	}}
	if _, _, err := b.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{TailscaleCredentialsURL, `"name":"Mewla"`, `"dryRun":true`, `"id":"devices:core"`, `"id":"all:read"`} {
		if !strings.Contains(seen, want) {
			t.Fatalf("script lacks %s", want)
		}
	}
	if strings.Contains(seen, "__CONFIG__") || strings.Contains(strings.ToLower(seen), "password") && strings.Contains(seen, ".fill(") && strings.Contains(strings.ToLower(seen), "getbylabel(/password") {
		t.Fatal("script is not filled in or types a password")
	}
}
