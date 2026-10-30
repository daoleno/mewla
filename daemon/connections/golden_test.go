package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestGoldenIntegrationBehavior dumps what each integration is allowed to do
// and where it may go: catalog entries, readiness, reviewed tools and their
// permission classes, official MCP allowlists, write consent, how an account
// is added, and how a sign-in starts. The expectation in
// testdata/golden_integrations.json was generated before the per-integration
// data moved into one catalog; the dump must stay byte-identical.
//
// Regenerate deliberately with MEWLA_UPDATE_GOLDEN=1.
func TestGoldenIntegrationBehavior(t *testing.T) {
	oldGitHub, oldSlack := GitHubPublicClientID, SlackPublicClientID
	defer func() {
		GitHubPublicClientID, SlackPublicClientID = oldGitHub, oldSlack
	}()
	GitHubPublicClientID, SlackPublicClientID = "", ""

	ids := []string{}
	for _, item := range Catalog() {
		ids = append(ids, item.ID)
	}
	dump := map[string]any{
		"catalog":       Catalog(),
		"readiness":     goldenReadiness(t),
		"tools":         goldenTools(),
		"official":      goldenOfficialGroups(ids),
		"write_consent": goldenWriteConsent(),
		"uses_mcp":      goldenUsesMCP(ids),
		"names":         goldenNames(ids),
		"add":           goldenAdd(t, ids),
		"slack_scopes":  goldenSlackScopes(t),
		"defaults":      goldenRequestDefaults(t),
		"configure":     goldenConfigure(t, ids),
		"connect":       goldenConnect(t, ids),
	}
	got, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden_integrations.json")
	if os.Getenv("MEWLA_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		diff := filepath.Join(os.TempDir(), "golden_integrations.got.json")
		_ = os.WriteFile(diff, got, 0o644)
		t.Fatalf("integration behavior changed; compare %s with %s", diff, path)
	}
}

func goldenReadiness(t *testing.T) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	run := func(name string, setup func(m *Manager)) {
		m, err := New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		setup(m)
		reasons := map[string]string{}
		for _, item := range m.catalog() {
			reasons[item.ID] = item.UnavailableReason
			if item.Available != (item.UnavailableReason == "") {
				t.Fatalf("%s availability disagrees with its reason", item.ID)
			}
		}
		out[name] = reasons
	}
	vault := func(m *Manager, kind string, c OAuthClientConfig) {
		raw, _ := json.Marshal(c)
		if err := m.vault.Set("oauth-client:"+kind, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	run("none", func(*Manager) {})
	run("publisher", func(*Manager) {
		GitHubPublicClientID, SlackPublicClientID = "gh", "sl"
	})
	GitHubPublicClientID, SlackPublicClientID = "", ""
	run("operator_github", func(m *Manager) { vault(m, "github", OAuthClientConfig{ClientID: "v"}) })
	run("operator_slack_app", func(m *Manager) { vault(m, "slack", OAuthClientConfig{ClientID: "v", RedirectURL: NativeCallback}) })
	run("operator_slack_relay", func(m *Manager) { vault(m, "slack", OAuthClientConfig{ClientID: "v", RedirectURL: NativeReturnRelay}) })
	run("operator_slack_secret_callback", func(m *Manager) {
		vault(m, "slack", OAuthClientConfig{ClientID: "v", ClientSecret: "s", RedirectURL: "https://d.example/plugins/oauth/callback"})
	})
	run("operator_slack_secret_app", func(m *Manager) {
		vault(m, "slack", OAuthClientConfig{ClientID: "v", ClientSecret: "s", RedirectURL: NativeCallback})
	})
	run("operator_slack_no_secret_callback", func(m *Manager) {
		vault(m, "slack", OAuthClientConfig{ClientID: "v", RedirectURL: "https://d.example/plugins/oauth/callback"})
	})
	run("operator_notion", func(m *Manager) { vault(m, "notion", OAuthClientConfig{ClientID: "v"}) })
	return out
}

type goldenTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Read        bool   `json:"read"`
	Fingerprint string `json:"fingerprint"`
	Group       string `json:"group"`
	GroupOAuth  string `json:"group_oauth"`
}

func goldenTools() map[string][]goldenTool {
	out := map[string][]goldenTool{}
	for _, id := range []string{"github", "notion", "slack", "hyperliquid"} {
		plain := &record{Account: Account{Integration: id}}
		oauth := &record{Account: Account{Integration: id, AuthMethod: "oauth"}}
		list := []goldenTool{}
		for _, tool := range goldenReviewed(id) {
			list = append(list, goldenTool{tool.Name, tool.Description, tool.Method, tool.Path, tool.Read, fingerprint(tool), toolGroup(plain, tool), toolGroup(oauth, tool)})
		}
		out[id] = list
	}
	return out
}

func goldenOfficialGroups(ids []string) map[string]any {
	out := map[string]any{}
	official := goldenOfficial()
	officialIDs := []string{}
	for id := range official {
		officialIDs = append(officialIDs, id)
	}
	sort.Strings(officialIDs)
	for _, id := range officialIDs {
		set := official[id]
		probes := append(append(append([]string{}, set.Reads...), set.Writes...), "unknown_tool", "get_me", "save_something_new")
		groups := func(authMethod, endpoint string) map[string]string {
			r := &record{Account: Account{Integration: id, AuthMethod: authMethod, Endpoint: endpoint}}
			g := map[string]string{}
			for _, name := range probes {
				g[name] = toolGroup(r, Tool{Name: name})
			}
			return g
		}
		out[id] = map[string]any{
			"set":            set,
			"oauth":          groups("mcp_oauth", set.Endpoint),
			"oauth_other":    groups("mcp_oauth", "https://other.example/mcp"),
			"no_auth_method": groups("", set.Endpoint),
		}
	}
	// Every other integration answers no official group, whatever it is called.
	stray := map[string]map[string]string{}
	for _, id := range ids {
		if _, ok := official[id]; ok {
			continue
		}
		r := &record{Account: Account{Integration: id, AuthMethod: "mcp_oauth", Endpoint: "https://mcp.notion.com/mcp"}}
		stray[id] = map[string]string{"get_me": toolGroup(r, Tool{Name: "get_me"}), "notion-search": toolGroup(r, Tool{Name: "notion-search"}), "execute": toolGroup(r, Tool{Name: "execute"})}
	}
	out["others"] = stray
	return out
}

func goldenWriteConsent() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, id := range []string{"slack", "linear", "github", "notion", "onlora", "cloudflare", "mcp", "hyperliquid"} {
		scopeSets := map[string][]string{
			"none":  nil,
			"read":  {"channels:read", "read", "openid"},
			"chat":  {"chat:write"},
			"write": {"write"},
		}
		results := map[string]bool{}
		for name, scopes := range scopeSets {
			r := &record{Account: Account{Integration: id, Scopes: scopes}, OAuth: &oauthAccount{}}
			results[name] = writeConsent(r)
		}
		results["no_oauth"] = writeConsent(&record{Account: Account{Integration: id}})
		out[id] = results
	}
	return out
}

func goldenUsesMCP(ids []string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, id := range ids {
		out[id] = map[string]bool{}
		for _, method := range []string{"", "oauth", "mcp_oauth"} {
			out[id]["method:"+method] = usesMCP(&record{Account: Account{Integration: id, AuthMethod: method}})
		}
	}
	return out
}

func goldenNames(ids []string) map[string]any {
	out := map[string]any{}
	for _, id := range append(append([]string{}, ids...), "bogus", "") {
		out[id] = map[string]string{
			"service":      serviceName(id),
			"account":      accountService(&record{Account: Account{Integration: id}}),
			"account_name": accountService(&record{Account: Account{Integration: id, Name: "Mine"}}),
		}
	}
	return out
}

// goldenWire answers every request a scenario sends and records it.
type goldenWire struct {
	t        *testing.T
	requests []string
	respond  func(req *http.Request, body []byte) (int, http.Header, string)
}

func (w *goldenWire) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	line := req.Method + " " + req.URL.String()
	for _, header := range []string{"Notion-Version", "X-GitHub-Api-Version", "Authorization", "Accept"} {
		if v := req.Header.Get(header); v != "" && header != "Accept" {
			line += " " + header + "=" + v
		}
	}
	if len(body) > 0 && req.Header.Get("Content-Type") == "application/json" && !strings.Contains(req.URL.Path, "mcp") {
		line += " body=" + string(body)
	}
	if strings.Contains(req.URL.Path, "register") {
		line = req.Method + " " + req.URL.String() + " body=" + canonicalJSON(body)
	}
	line = randomFields.ReplaceAllString(line, `"$1":"<random>"`)
	w.requests = append(w.requests, line)
	status, header, text := w.respond(req, body)
	if header == nil {
		header = http.Header{}
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(text)), Request: req}, nil
}

func (w *goldenWire) seen() []string {
	unique := map[string]bool{}
	for _, line := range w.requests {
		unique[line] = true
	}
	return sortedKeys(unique)
}

func canonicalJSON(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// randomFields are per-flow secrets a request carries; their values change on every run.
var randomFields = regexp.MustCompile(`"(public_key|nonce)":"[^"]*"`)

func goldenServices(t *testing.T, slackScopes string, slackBot bool) *goldenWire {
	return &goldenWire{t: t, respond: func(req *http.Request, body []byte) (int, http.Header, string) {
		switch req.URL.Host + req.URL.Path {
		case "api.github.com/user":
			return 200, nil, `{"login":"octo","id":1}`
		case "api.notion.com/v1/users/me":
			return 200, nil, `{"id":"u1","bot":{"workspace_name":"Workspace"}}`
		case "slack.com/api/auth.test":
			bot := ""
			if slackBot {
				bot = `,"bot_id":"B1"`
			}
			return 200, http.Header{"X-Oauth-Scopes": {slackScopes}}, `{"ok":true,"team":"Team","user":"User","user_id":"U1"` + bot + `}`
		case "slack.com/api/conversations.list", "slack.com/api/user":
			return 200, nil, `{"ok":false}`
		case "api.hyperliquid.xyz/info", "api.hyperliquid-testnet.xyz/info":
			return 200, nil, `{"marginSummary":{"accountValue":"0"},"assetPositions":[]}`
		case "api.github.com/user/repos", "api.notion.com/v1/blocks/b1/children":
			return 200, nil, `[]`
		}
		return 404, nil, `{}`
	}}
}

func goldenAccount(m *Manager, a *Account) map[string]any {
	if a == nil {
		return nil
	}
	projected := m.projection(m.records[a.ID])
	tools := []map[string]any{}
	for _, tool := range projected.Tools {
		tools = append(tools, map[string]any{"name": tool.Name, "allowed": tool.Allowed, "group": tool.Group, "fingerprint": fingerprint(tool)})
	}
	return map[string]any{"integration": projected.Integration, "name": projected.Name, "identity": projected.Identity, "endpoint": projected.Endpoint, "trusted_networks": projected.TrustedNetworks, "auth_method": projected.AuthMethod, "scopes": projected.Scopes, "network": projected.Network, "address": projected.Address, "enabled": projected.Enabled, "status": projected.Status, "access": projected.Access, "tools": tools}
}

func goldenAdd(t *testing.T, ids []string) map[string]any {
	t.Helper()
	spec := json.RawMessage(`{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{"/ping":{"get":{"operationId":"ping","responses":{"200":{"description":"ok"}}}}}}`)
	inputs := map[string]func(id string) *Input{
		"credential": func(id string) *Input {
			in := &Input{Integration: id, Name: "Name", Credential: "tok-123", Endpoint: "https://custom.example/mcp"}
			if id == "openapi" {
				in.Spec = spec
			}
			return in
		},
		"no_credential": func(id string) *Input {
			in := &Input{Integration: id, Name: "Name", Endpoint: "https://custom.example/mcp"}
			if id == "openapi" {
				in.Spec = spec
			}
			return in
		},
		"trusted": func(id string) *Input {
			in := &Input{Integration: id, Name: "Name", Credential: "tok-123", Endpoint: "https://custom.example/mcp", TrustedNetworks: []string{"10.0.0.0/8"}}
			if id == "openapi" {
				in.Spec = spec
			}
			return in
		},
		"unnamed": func(id string) *Input { return &Input{Integration: id, Credential: "tok-123"} },
		"spec_on_other": func(id string) *Input {
			return &Input{Integration: id, Name: "Name", Credential: "tok-123", Spec: spec}
		},
	}
	out := map[string]any{}
	for _, id := range append(append([]string{}, ids...), "bogus") {
		byInput := map[string]any{}
		for name, build := range inputs {
			wire := goldenServices(t, "channels:read,chat:write", false)
			m := testManager(t, wire.RoundTrip)
			response, err := m.Handle(context.Background(), Request{Action: "add", Input: build(id)})
			entry := map[string]any{"requests": wire.seen()}
			if err != nil {
				entry["error"] = err.Error()
			} else {
				entry["account"] = goldenAccount(m, response.Account)
			}
			byInput[name] = entry
		}
		out[id] = byInput
	}
	// Removing a key from an account that has none.
	wire := goldenServices(t, "", false)
	m := testManager(t, wire.RoundTrip)
	account := addGitHub(t, m, "Octo", "tok")
	_, err := m.Handle(context.Background(), Request{Action: "remove_key", ID: account.ID})
	out["remove_key_on_github"] = err.Error()
	return out
}

func goldenSlackScopes(t *testing.T) map[string]any {
	t.Helper()
	out := map[string]any{}
	for name, variant := range map[string]struct {
		scopes string
		bot    bool
	}{
		"read":      {"channels:read,groups:read,search:read", false},
		"all":       {"channels:read,channels:history,search:read,chat:write", false},
		"bot":       {"channels:read,channels:history,search:read,chat:write", true},
		"history":   {"im:history", false},
		"chat_user": {"chat:write:user", false},
		"none":      {"", false},
	} {
		wire := goldenServices(t, variant.scopes, variant.bot)
		m := testManager(t, wire.RoundTrip)
		response, err := m.Handle(context.Background(), Request{Action: "add", Input: &Input{Integration: "slack", Name: "S", Credential: "xoxp"}})
		if err != nil {
			out[name] = err.Error()
			continue
		}
		out[name] = goldenAccount(m, response.Account)
	}
	return out
}

func goldenRequestDefaults(t *testing.T) map[string]any {
	t.Helper()
	out := map[string]any{}
	wire := goldenServices(t, "", false)
	m := testManager(t, wire.RoundTrip)
	gh := addGitHub(t, m, "Octo", "tok")
	mustHandle(t, m, Request{Action: "invoke", ID: gh.ID, Tool: "list_repositories"})
	mustHandle(t, m, Request{Action: "invoke", ID: gh.ID, Tool: "list_repositories", Arguments: json.RawMessage(`{"query":{"per_page":5}}`)})
	notion := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "notion", Name: "N", Credential: "ntn"}}).Account
	mustHandle(t, m, Request{Action: "invoke", ID: notion.ID, Tool: "list_block_children", Arguments: json.RawMessage(`{"path":{"id":"b1"}}`)})
	// A custom OpenAPI plugin pointed at a known host gets that host's headers.
	spec := json.RawMessage(`{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{"/user":{"get":{"operationId":"me","responses":{"200":{"description":"ok"}}}}}}`)
	for _, endpoint := range []string{"https://api.github.com", "https://api.notion.com/v1", "https://slack.com/api", "https://other.example"} {
		api := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "openapi", Name: "O " + endpoint, Endpoint: endpoint, Spec: spec}}).Account
		mustHandle(t, m, Request{Action: "policy", ID: api.ID, Tool: "me", Allowed: true})
		_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: api.ID, Tool: "me"})
		if err != nil {
			out["openapi "+endpoint] = err.Error()
		}
	}
	out["requests"] = wire.seen()
	return out
}

func goldenConfigure(t *testing.T, ids []string) map[string]any {
	t.Helper()
	valid := "https://daemon.example/plugins/oauth/callback"
	clients := map[string]*OAuthClientConfig{
		"id_valid":         {ClientID: "c", RedirectURL: valid},
		"id_secret_valid":  {ClientID: "c", ClientSecret: "s", RedirectURL: valid},
		"id_no_redirect":   {ClientID: "c"},
		"id_app":           {ClientID: "c", RedirectURL: NativeCallback},
		"id_secret_app":    {ClientID: "c", ClientSecret: "s", RedirectURL: NativeCallback},
		"no_id_valid":      {RedirectURL: valid},
		"no_id_app":        {RedirectURL: NativeCallback},
		"no_id_no_address": {},
		"mcp_resource":     {ClientID: "c", RedirectURL: valid, ResourceURL: "https://custom.example/mcp"},
		"nil":              nil,
	}
	out := map[string]any{}
	for _, id := range append(append([]string{}, ids...), "bogus") {
		byClient := map[string]any{}
		for name, client := range clients {
			m, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			response, err := m.Handle(context.Background(), Request{Action: "oauth_configure", Input: &Input{Integration: id, OAuthClient: client}})
			if err != nil {
				byClient[name] = err.Error()
				continue
			}
			byClient[name] = sortedCopy(response.OAuthConfigured)
		}
		out[id] = byClient
	}
	// Every integration configured in turn. The list is a set; its order is
	// not part of the contract, so the dump sorts it.
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		_, _ = m.Handle(context.Background(), Request{Action: "oauth_configure", Input: &Input{Integration: id, OAuthClient: clients["id_secret_valid"]}})
	}
	out["all_configured"] = sortedCopy(m.oauthConfigured())
	return out
}

func sortedCopy(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)
	return out
}

// goldenSignIn answers MCP OAuth discovery for any resource, registers a
// client, and serves GitHub's device code.
func goldenSignIn(t *testing.T) *goldenWire {
	return &goldenWire{t: t, respond: func(req *http.Request, body []byte) (int, http.Header, string) {
		const resourcePrefix = "/.well-known/oauth-protected-resource"
		switch {
		case req.Method == "GET" && strings.HasPrefix(req.URL.Path, resourcePrefix):
			resource := "https://" + req.URL.Host + strings.TrimPrefix(req.URL.Path, resourcePrefix)
			raw, _ := json.Marshal(map[string]any{"resource": resource, "authorization_servers": []string{"https://auth.example"}})
			return 200, nil, string(raw)
		case req.Method == "GET" && req.URL.Host == "auth.example" && strings.HasPrefix(req.URL.Path, "/.well-known/oauth-authorization-server"):
			return 200, nil, `{"issuer":"https://auth.example","authorization_endpoint":"https://auth.example/authorize","token_endpoint":"https://auth.example/token","registration_endpoint":"https://auth.example/register","revocation_endpoint":"https://auth.example/revoke","scopes_supported":["read","write","openid","email","profile","offline_access"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none","client_secret_basic"]}`
		case req.Method == "POST" && req.URL.String() == "https://auth.example/register":
			return 200, nil, `{"client_id":"dynamic-client","token_endpoint_auth_method":"none"}`
		case req.Method == "POST" && req.URL.String() == "https://github.com/login/device/code":
			return 200, nil, `{"device_code":"dev","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`
		}
		return 404, nil, `{}`
	}}
}

func normalizedAuthorization(raw string) any {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := u.Query()
	for _, key := range []string{"state", "code_challenge"} {
		if query.Get(key) != "" {
			query.Set(key, "<"+key+">")
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func goldenConnect(t *testing.T, ids []string) map[string]any {
	t.Helper()
	type variant struct {
		action     string
		webOrigin  string
		writes     bool
		mobile     bool
		publishers bool
		operator   *OAuthClientConfig
		endpoint   string
		trusted    []string
	}
	custom := "https://custom.example/mcp"
	variants := map[string]variant{
		"native":                   {action: "connect_start"},
		"native_writes_publishers": {action: "connect_start", writes: true, publishers: true},
		"web_writes_publishers":    {action: "connect_start", webOrigin: "https://mewla.example", writes: true, publishers: true},
		"web":                      {action: "connect_start", webOrigin: "https://mewla.example"},
		"web_bad_origin":           {action: "connect_start", webOrigin: "https://evil.example"},
		"native_custom_endpoint":   {action: "connect_start", endpoint: custom},
		"native_operator_secret":   {action: "connect_start", endpoint: custom, operator: &OAuthClientConfig{ClientID: "op", ClientSecret: "sec", RedirectURL: "https://daemon.example/plugins/oauth/callback", ResourceURL: custom}},
		"direct":                   {action: "oauth_start"},
		"direct_writes":            {action: "oauth_start", writes: true},
		"direct_mobile":            {action: "oauth_start", mobile: true, writes: true},
		"direct_custom":            {action: "oauth_start", endpoint: custom},
		"direct_trusted":           {action: "oauth_start", endpoint: custom, trusted: []string{"10.0.0.0/8"}},
		"direct_operator":          {action: "oauth_start", endpoint: custom, operator: &OAuthClientConfig{ClientID: "op", RedirectURL: "https://daemon.example/plugins/oauth/callback", ResourceURL: custom}},
		"direct_operator_other":    {action: "oauth_start", endpoint: "https://elsewhere.example/mcp", operator: &OAuthClientConfig{ClientID: "op", RedirectURL: "https://daemon.example/plugins/oauth/callback", ResourceURL: custom}},
		"direct_unnamed_long":      {action: "oauth_start", endpoint: custom, writes: true},
	}
	out := map[string]any{}
	for _, id := range append(append([]string{}, ids...), "bogus") {
		byVariant := map[string]any{}
		for name, v := range variants {
			GitHubPublicClientID, SlackPublicClientID = "", ""
			if v.publishers {
				GitHubPublicClientID, SlackPublicClientID = "gh", "sl"
			}
			m, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			wire := goldenSignIn(t)
			m.networkOwned = false
			m.http = &http.Client{Transport: wire}
			m.SetLoopbackPort(9877)
			m.SetWebOrigins(func(origin string) bool { return origin == "https://mewla.example" })
			if v.operator != nil {
				raw, _ := json.Marshal(v.operator)
				_ = m.vault.Set("oauth-client:"+id, string(raw))
			}
			input := &Input{Integration: id, Endpoint: v.endpoint, AllowWrites: v.writes, Mobile: v.mobile, WebOrigin: v.webOrigin, TrustedNetworks: v.trusted}
			if name == "direct_unnamed_long" {
				input.Name = strings.Repeat("n", 81)
			}
			response, err := m.Handle(context.Background(), Request{Action: v.action, Input: input})
			entry := map[string]any{"requests": wire.seen()}
			if err != nil {
				entry["error"] = err.Error()
				byVariant[name] = entry
				continue
			}
			if response.Flow != nil {
				f := response.Flow
				entry["flow"] = map[string]any{"integration": f.Integration, "status": f.Status, "authorization_url": normalizedAuthorization(f.AuthorizationURL), "user_code": f.UserCode, "web_return": f.WebReturn, "has_account": f.AccountID != ""}
			}
			if response.AuthorizationURL != "" {
				entry["authorization_url"] = normalizedAuthorization(response.AuthorizationURL)
			}
			accountID := ""
			if response.Account != nil {
				accountID = response.Account.ID
			} else if response.Flow != nil {
				accountID = response.Flow.AccountID
			}
			if r := m.records[accountID]; r != nil {
				entry["account"] = map[string]any{"integration": r.Account.Integration, "name": r.Account.Name, "endpoint": r.Account.Endpoint, "auth_method": r.Account.AuthMethod, "scopes": r.Account.Scopes, "status": r.Account.Status, "trusted_networks": r.Account.TrustedNetworks, "enabled": r.Account.Enabled}
				if r.OAuth != nil {
					entry["oauth"] = map[string]any{"client_id": r.OAuth.ClientID, "redirect_url": r.OAuth.RedirectURL, "issuer": r.OAuth.Issuer, "auth_url": r.OAuth.AuthURL, "token_url": r.OAuth.TokenURL, "revoke_url": r.OAuth.RevokeURL, "resource": r.OAuth.Resource, "auth_style": r.OAuth.AuthStyle}
				}
			}
			pending := []map[string]any{}
			for _, flow := range m.pending {
				pending = append(pending, map[string]any{"mobile": flow.Mobile, "web": flow.Web, "allow_writes": flow.AllowWrites, "client_secret": flow.ClientSecret != ""})
			}
			entry["pending"] = pending
			byVariant[name] = entry
		}
		out[id] = byVariant
	}
	GitHubPublicClientID, SlackPublicClientID = "", ""
	return out
}
