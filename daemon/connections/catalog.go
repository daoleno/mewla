package connections

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/daoleno/mewla/daemon/googleauth"
	"golang.org/x/oauth2"
)

// The catalog is the one place that says how each integration differs from
// the others: where it lives, how a person signs in, which tools Mewla has
// reviewed and what class each one is, what the network layer adds to its
// requests. The generic code reads an entry; it never asks "which service is
// this". Behaviour that is code rather than data (Hyperliquid signing, Google
// and Slack token details, MCP and OpenAPI discovery) is a driver or a token
// codec, registered below and named by the entry that uses it.
//
// Adding an integration is adding one entry here, plus a driver or codec when
// it needs code the existing ones do not provide. The trust model does not
// live in the entries: tools are refused unless reviewed, a grant is bound to
// the full definition, and an official MCP list applies only on its endpoint.

// The official MCP services Mewla reviews.
const (
	notionMCP     = "https://mcp.notion.com/mcp"
	linearMCP     = "https://mcp.linear.app/mcp"
	onloraMCP     = "https://api.onlora.ai/mcp"
	cloudflareMCP = "https://mcp.cloudflare.com/mcp"

	hlMainnetAPI = "https://api.hyperliquid.xyz"
	hlTestnetAPI = "https://api.hyperliquid-testnet.xyz"

	googleScopeBase = "https://www.googleapis.com/auth/"
	// Both Google services name the account at the same OpenID Connect host.
	googleIdentityHost = "https://openidconnect.googleapis.com"
)

// Onlora registers Mewla's app as the public client "mewla", returning to
// mewla://plugins; a web UI return registers a client of its own on connect.
const onloraAppClientID = "mewla"

// Onlora's scopes are areas, not read and write: every tool needs one of them,
// and offline_access keeps the sign-in past the hour. What Brain may change is
// still each tool's own grant.
var onloraScopes = []string{"projects", "generate", "offline_access"}

// Cloudflare registers Mewla on the first connect. Mewla asks only for the
// scopes every grant needs; Cloudflare's consent page picks the rest,
// read-only unless the person chooses more.
var cloudflareScopes = []string{"user:read", "account:read", "offline_access"}

// clientSource says whose OAuth client a sign-in uses.
type clientSource int

const (
	// clientPublisher: Mewla's published app, set in the release build (or
	// entered by the server owner). Without one the service is not set up.
	clientPublisher clientSource = iota + 1
	// clientRegistered: Mewla registers its own client with the service on
	// every connect; an owner-entered client is not consulted for a return.
	clientRegistered
	// clientOperator: the server owner supplies a client, or the service
	// registers one dynamically.
	clientOperator
)

// redirectRule is a return address an owner may save for a client besides a
// registered daemon callback.
type redirectRule struct {
	URL        string
	PublicOnly bool // only for a client without a secret
}

// staticOAuth is a service whose endpoints are fixed instead of discovered.
type staticOAuth struct {
	Issuer, AuthURL, TokenURL string
	AuthMethods               []string
	// ScopeParam names the authorization parameter that carries the scopes
	// when the service does not use the standard "scope".
	ScopeParam string
}

// deviceFlow is a service that signs in with the OAuth device grant.
type deviceFlow struct {
	Issuer, CodeURL, TokenURL, Verification string
	Scopes                                  []string
}

type oauthSpec struct {
	Source clientSource
	// Public is the published client, when the release build carries one.
	Public func() (OAuthClientConfig, bool)
	// Configurable: the server owner may save a client for this service.
	Configurable bool
	// PublisherClientID: a saved client must carry a client ID.
	PublisherClientID bool
	ExtraRedirect     *redirectRule
	// ReturnOK judges whether a client's return address can finish a
	// sign-in; nil accepts any.
	ReturnOK func(OAuthClientConfig) bool
	// NativeClientID: the phone app signs in as this registered client.
	NativeClientID string
	// NativeViaRelay: the service refuses mewla://, so the app returns
	// through Mewla's https page.
	NativeViaRelay bool
	// Scopes is what a sign-in asks for; nil asks for the service's normal
	// read scope (and write, if changes were requested) from its metadata.
	Scopes func(writes bool) []string
	// ChangeScopes must be in a grant before changes can be allowed; when one
	// is missing the service has to ask the person again.
	ChangeScopes func() []string
	Static       *staticOAuth
	Device       *deviceFlow
	// Codec names a registered token codec for services that do not follow
	// the standard token endpoint.
	Codec string
}

// mcpSpec is a service reached over its official remote MCP server. Its tools
// are reviewed by name and only on this exact endpoint.
type mcpSpec struct {
	Endpoint string
	Reads    []string
	Writes   []string
	// Identity names a reviewed read tool that says who is signed in, and the
	// arguments it takes (each must be a property of its input schema).
	Identity     string
	IdentityArgs map[string]any
}

type queryDefault struct {
	ToolPrefix string
	Tool       string
	Param      string
	Value      string
}

type googleSpec struct {
	// Service is the exchange service whose scopes a sign-in asks for.
	Service      string
	IdentityHost string
	Ops          func() []googleTool
}

type entry struct {
	Integration

	// Driver names the registered code that discovers and calls an account
	// added with a credential.
	Driver string
	// Custom: the person supplies the endpoint and may trust internal
	// networks. A custom service has no reviewed tools.
	Custom bool
	// Endpoint is the service's API origin.
	Endpoint string
	// AddRefusal: the service connects only through browser sign-in; adding
	// it with a credential says this.
	AddRefusal      string
	NeedsCredential bool
	// TakesSpec: the service is described by an uploaded OpenAPI document.
	TakesSpec bool
	// NamedByPerson: the service has no name of its own; the person's name for
	// the account stands for it in messages.
	NamedByPerson bool
	// TokenMCP: an account added with a credential speaks MCP.
	TokenMCP bool
	// IdentityPath is the REST call that names the signed-in account.
	IdentityPath string
	// Tools are the reviewed built-in tool definitions.
	Tools         func() []Tool
	Headers       map[string]string
	OKEnvelope    bool // the service answers 200 with {"ok": false} on failure
	QueryDefaults []queryDefault
	// Networks maps a network name to its API origin (Hyperliquid).
	Networks map[string]string
	Google   *googleSpec
	MCP      *mcpSpec
	OAuth    *oauthSpec
}

// driver is the code behind an entry.
type driver struct {
	// discover reads the account's identity and returns its tool list.
	discover func(m *Manager, ctx context.Context, r *record, secret string) ([]Tool, error)
	// invoke runs one tool. Nil for MCP, which has its own call path.
	invoke func(m *Manager, ctx context.Context, r *record, secret string, t Tool, args map[string]any, capture ...*http.Header) ([]byte, error)
	// unverified: discovery proves nothing about the account (an uploaded
	// OpenAPI document), so a new account is "configured", not "connected".
	unverified bool
	// add replaces the generic add flow.
	add func(m *Manager, ctx context.Context, in *Input) (Response, error)
	// removeKey deletes a saved trading key, for services that have one.
	removeKey func(m *Manager, r *record) error
}

// The registries are filled in init: the drivers call back into the catalog,
// which a package-level initializer cannot do.
var drivers map[string]driver

func registerDrivers() {
	drivers = map[string]driver{
		"rest":        {discover: discoverREST, invoke: invokeREST},
		"slack":       {discover: discoverSlack, invoke: invokeREST},
		"google":      {discover: discoverGoogleDriver, invoke: invokeGoogleDriver},
		"hyperliquid": {discover: discoverHyperliquidDriver, invoke: invokeHyperliquidDriver, add: addHyperliquidDriver, removeKey: removeHyperliquidKeyDriver},
		"openapi":     {discover: discoverOpenAPI, invoke: invokeOpenAPI, unverified: true},
		"mcp":         {discover: discoverMCPToken},
	}
}

// tokenCodec is a service's own token exchange, refresh and revocation.
type tokenCodec struct {
	exchange func(m *Manager, ctx context.Context, r *record, secret, code, verifier string) (*oauth2.Token, error)
	refresh  func(m *Manager, ctx context.Context, r *record, refreshToken string) (*oauth2.Token, error)
	revoke   func(m *Manager, ctx context.Context, r *record, accessToken string) error
}

var codecs map[string]tokenCodec

var (
	integrations []*entry
	byID         map[string]*entry
	hostRules    map[string]*entry
)

func init() {
	registerDrivers()
	codecs = map[string]tokenCodec{"slack": slackCodec}
	integrations = buildCatalog()
	byID = map[string]*entry{}
	hostRules = map[string]*entry{}
	for _, e := range integrations {
		if err := e.validate(); err != nil {
			panic(fmt.Sprintf("connections catalog: %s: %v", e.ID, err))
		}
		if byID[e.ID] != nil {
			panic("connections catalog: duplicate integration " + e.ID)
		}
		byID[e.ID] = e
		if e.Endpoint != "" && (len(e.Headers) > 0 || e.OKEnvelope) {
			u, _ := url.Parse(e.Endpoint)
			hostRules[u.Hostname()] = e
		}
	}
}

func buildCatalog() []*entry {
	return []*entry{
		{
			Integration: Integration{ID: "github", Name: "GitHub", Available: true, SetupURL: "https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps", Description: "Repositories, issues and pull requests"},
			Driver:      "rest", Endpoint: "https://api.github.com", NeedsCredential: true, IdentityPath: "/user", Tools: githubTools,
			Headers:       map[string]string{"X-GitHub-Api-Version": "2022-11-28"},
			QueryDefaults: []queryDefault{{ToolPrefix: "list_", Param: "per_page", Value: "30"}},
			OAuth: &oauthSpec{
				Source: clientPublisher, Configurable: true, PublisherClientID: true,
				Public: func() (OAuthClientConfig, bool) {
					return OAuthClientConfig{ClientID: GitHubPublicClientID}, GitHubPublicClientID != ""
				},
				ExtraRedirect: &redirectRule{URL: ""},
				Device:        &deviceFlow{Issuer: "https://github.com", CodeURL: "https://github.com/login/device/code", TokenURL: "https://github.com/login/oauth/access_token", Verification: "https://github.com/login/device", Scopes: []string{"read:user", "repo"}},
			},
		},
		{
			Integration: Integration{ID: "notion", Name: "Notion", Available: true, SetupURL: "https://www.notion.so/profile/integrations", Description: "Pages, search and workspace content"},
			Driver:      "rest", Endpoint: "https://api.notion.com/v1", NeedsCredential: true, IdentityPath: "/users/me", Tools: notionTools,
			Headers:       map[string]string{"Notion-Version": "2022-06-28"},
			QueryDefaults: []queryDefault{{Tool: "list_block_children", Param: "page_size", Value: "30"}},
			MCP: &mcpSpec{
				Endpoint: notionMCP,
				Reads:    []string{"notion-search", "notion-fetch", "notion-get-comments", "notion-get-users", "notion-get-self", "notion-get-teams"},
				Writes:   []string{"notion-create-pages", "notion-update-page", "notion-create-comment", "notion-move-pages", "notion-duplicate-page"},
				Identity: "notion-get-self",
			},
			OAuth: &oauthSpec{Source: clientRegistered, Configurable: true},
		},
		{
			Integration: Integration{ID: "google-calendar", Name: "Google Calendar", Available: true, SetupURL: "https://calendar.google.com", Description: "Calendars, events and free time"},
			Driver:      "google", Endpoint: "https://www.googleapis.com",
			AddRefusal: "Google uses browser authorization; start OAuth for this account",
			Google:     &googleSpec{Service: googleauth.Calendar, IdentityHost: googleIdentityHost, Ops: calendarTools},
		},
		{
			Integration: Integration{ID: "gmail", Name: "Gmail", Available: true, SetupURL: "https://mail.google.com", Description: "Search, read and draft email"},
			Driver:      "google", Endpoint: "https://gmail.googleapis.com",
			AddRefusal: "Google uses browser authorization; start OAuth for this account",
			Google:     &googleSpec{Service: googleauth.Gmail, IdentityHost: googleIdentityHost, Ops: gmailTools},
		},
		{
			Integration: Integration{ID: "slack", Name: "Slack", Available: true, SetupURL: "https://api.slack.com/apps", Description: "Channels, conversations and messages"},
			Driver:      "slack", Endpoint: "https://slack.com/api", NeedsCredential: true, Tools: slackTools, OKEnvelope: true,
			OAuth: &oauthSpec{
				Source: clientPublisher, Configurable: true, PublisherClientID: true,
				// Public distribution requires https on every Slack redirect
				// URL, so the app's sign-in returns through Mewla's page to
				// mewla://plugins.
				Public: func() (OAuthClientConfig, bool) {
					return OAuthClientConfig{ClientID: SlackPublicClientID, RedirectURL: NativeReturnRelay}, SlackPublicClientID != ""
				},
				ExtraRedirect: &redirectRule{URL: NativeCallback, PublicOnly: true},
				ReturnOK: func(c OAuthClientConfig) bool {
					return appReturn(c.RedirectURL) && c.ClientSecret == "" || validCallback(c.RedirectURL) && c.ClientSecret != ""
				},
				Static: &staticOAuth{Issuer: "https://slack.com", AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access", AuthMethods: []string{"client_secret_post"}, ScopeParam: "user_scope"},
				Scopes: func(writes bool) []string {
					scopes := []string{"channels:read", "channels:history", "search:read"}
					if writes {
						scopes = append(scopes, "chat:write")
					}
					return scopes
				},
				ChangeScopes: func() []string { return []string{"chat:write"} },
				Codec:        "slack",
			},
		},
		{
			Integration: Integration{ID: "hyperliquid", Name: "Hyperliquid", Available: true, SetupURL: "https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api", Description: "Markets and any address's positions, with no setup; trading with an API wallet key you add"},
			Driver:      "hyperliquid", Tools: hyperliquidTools,
			Networks: map[string]string{"mainnet": hlMainnetAPI, "testnet": hlTestnetAPI},
		},
		{
			Integration: Integration{ID: "linear", Name: "Linear", Available: true, SetupURL: "https://linear.app/settings/account/security", Description: "Issues, projects and team activity"},
			Driver:      "mcp", Endpoint: linearMCP, NeedsCredential: true, TokenMCP: true,
			// Linear's MCP server now saves (creates or updates) through save_* tools.
			MCP: &mcpSpec{
				Endpoint: linearMCP,
				Reads:    []string{"get_user", "list_users", "get_issue", "list_issues", "get_project", "list_projects", "list_teams", "get_team", "list_comments", "list_cycles", "list_issue_statuses", "get_issue_status", "list_issue_labels", "get_document", "list_documents", "get_project_status", "list_project_statuses", "list_milestones", "get_milestone", "list_project_labels", "get_status_updates"},
				Writes:   []string{"create_issue", "update_issue", "create_comment", "update_comment", "create_project", "update_project", "create_document", "update_document", "save_issue", "save_comment", "save_project", "save_document", "save_milestone", "save_status_update"},
				Identity: "get_user", IdentityArgs: map[string]any{"query": "me"},
			},
			OAuth: &oauthSpec{Source: clientRegistered, Configurable: true, ChangeScopes: func() []string { return []string{"write"} }},
		},
		{
			Integration: Integration{ID: "onlora", Name: "Onlora", Available: true, SetupURL: "https://onlora.ai", Description: "Asset generation and search: images, speech and openly licensed media, kept in projects"},
			AddRefusal:  "Onlora uses browser authorization; connect it from Extensions",
			MCP: &mcpSpec{
				Endpoint: onloraMCP,
				Reads:    []string{"list_projects", "get_project", "list_files", "read_file", "search_library", "get_asset", "get_job", "get_quota", "list_models"},
				// generate_image, edit_image and the paid generate_* tools spend
				// the account's quota (generate_voice is free unless a paid model
				// is named); delete_file removes a project file; update_asset
				// changes an asset's title or note.
				Writes: []string{"create_project", "update_project", "write_file", "delete_file", "add_asset", "update_asset", "create_upload", "generate_image", "edit_image", "generate_voice"},
			},
			OAuth: &oauthSpec{
				Source: clientRegistered, NativeClientID: onloraAppClientID,
				Scopes: func(bool) []string { return append([]string(nil), onloraScopes...) },
			},
		},
		{
			Integration: Integration{ID: "cloudflare", Name: "Cloudflare", Available: true, SetupURL: "https://github.com/cloudflare/mcp", Description: "The whole Cloudflare API: Workers, R2, D1, DNS and more"},
			AddRefusal:  "Cloudflare uses browser authorization; connect it from Extensions",
			MCP: &mcpSpec{
				Endpoint: cloudflareMCP,
				Reads:    []string{"search", "docs", "whoami"},
				// execute runs any Cloudflare API call the grant allows, changes included.
				Writes:   []string{"execute"},
				Identity: "whoami",
			},
			OAuth: &oauthSpec{
				Source: clientRegistered, NativeViaRelay: true,
				Scopes: func(bool) []string { return append([]string(nil), cloudflareScopes...) },
			},
		},
		{
			Integration: Integration{ID: "mcp", Name: "Remote MCP", Available: true, SetupURL: "https://modelcontextprotocol.io", Description: "Discover tools from a remote MCP server"},
			Driver:      "mcp", Custom: true, TokenMCP: true, NamedByPerson: true,
			OAuth: &oauthSpec{Source: clientOperator, Configurable: true},
		},
		{
			Integration: Integration{ID: "openapi", Name: "OpenAPI", Available: true, SetupURL: "https://spec.openapis.org/oas/v3.0.3", Description: "Discover operations from an OpenAPI 3 document"},
			Driver:      "openapi", Custom: true, TakesSpec: true,
		},
	}
}

func (e *entry) validate() error {
	if e.ID == "" || e.Name == "" || e.SetupURL == "" || e.Description == "" {
		return fmt.Errorf("id, name, setup URL and description are required")
	}
	if e.AddRefusal == "" && e.Driver == "" {
		return fmt.Errorf("an entry that can be added needs a driver")
	}
	if e.Driver != "" {
		d, ok := drivers[e.Driver]
		if !ok {
			return fmt.Errorf("unknown driver %q", e.Driver)
		}
		if e.TokenMCP != (e.Driver == "mcp") {
			return fmt.Errorf("token_mcp and the mcp driver go together")
		}
		if e.Driver != "mcp" && d.invoke == nil {
			return fmt.Errorf("driver %q cannot invoke", e.Driver)
		}
	}
	for _, origin := range append([]string{e.Endpoint}, networkOrigins(e)...) {
		if origin == "" {
			continue
		}
		if _, err := endpointURL(origin); err != nil {
			return fmt.Errorf("endpoint %q: %v", origin, err)
		}
	}
	if e.Custom && (e.Endpoint != "" || e.MCP != nil || e.Tools != nil || e.Google != nil) {
		return fmt.Errorf("a custom service has no fixed endpoint or reviewed tools")
	}
	if !e.Custom && e.Driver != "mcp" && e.Driver != "openapi" && e.Driver != "" && e.Endpoint == "" && len(e.Networks) == 0 {
		return fmt.Errorf("a built-in service needs an endpoint")
	}
	if e.NeedsCredential && e.Endpoint == "" {
		return fmt.Errorf("needs_credential requires an endpoint")
	}
	if (e.Driver == "rest" || e.Driver == "slack" || e.Driver == "hyperliquid") && e.Tools == nil {
		return fmt.Errorf("driver %q needs reviewed tools", e.Driver)
	}
	if e.Driver == "rest" && e.IdentityPath == "" {
		return fmt.Errorf("the rest driver needs an identity path")
	}
	if (e.Driver == "google") != (e.Google != nil) {
		return fmt.Errorf("the google driver and its spec go together")
	}
	if e.Google != nil {
		if e.Google.Service == "" || e.Google.Ops == nil {
			return fmt.Errorf("a google service needs its scopes name and operations")
		}
		if _, err := endpointURL(e.Google.IdentityHost); err != nil {
			return fmt.Errorf("identity host: %v", err)
		}
	}
	if e.MCP != nil {
		if _, err := endpointURL(e.MCP.Endpoint); err != nil {
			return fmt.Errorf("mcp endpoint: %v", err)
		}
		seen := map[string]bool{}
		for _, name := range append(append([]string{}, e.MCP.Reads...), e.MCP.Writes...) {
			if name == "" || seen[name] {
				return fmt.Errorf("mcp tool %q is empty or listed twice", name)
			}
			seen[name] = true
		}
		if e.MCP.Identity != "" && !contains(e.MCP.Reads, e.MCP.Identity) {
			return fmt.Errorf("the identity tool must be a reviewed read")
		}
	}
	if o := e.OAuth; o != nil {
		if o.Source == 0 {
			return fmt.Errorf("oauth needs a client source")
		}
		if o.Source == clientPublisher && o.Public == nil {
			return fmt.Errorf("a publisher client needs its published ID")
		}
		if o.Source == clientRegistered && e.MCP == nil {
			return fmt.Errorf("a registered client needs the MCP endpoint it signs in to")
		}
		if o.Static != nil && (e.Endpoint == "" || o.Static.AuthURL == "" || o.Static.TokenURL == "" || o.Scopes == nil) {
			return fmt.Errorf("a static oauth service needs endpoints and scopes")
		}
		if o.Codec != "" {
			if _, ok := codecs[o.Codec]; !ok {
				return fmt.Errorf("unknown token codec %q", o.Codec)
			}
		}
	}
	return nil
}

func networkOrigins(e *entry) []string {
	names := make([]string, 0, len(e.Networks))
	for name := range e.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []string{}
	for _, name := range names {
		out = append(out, e.Networks[name])
	}
	return out
}

// lookup returns the catalog entry for an integration, or nil.
func lookup(id string) *entry { return byID[id] }

// Catalog lists the integrations a person can connect.
func Catalog() []Integration {
	out := make([]Integration, 0, len(integrations))
	for _, e := range integrations {
		out = append(out, e.Integration)
	}
	return out
}

func serviceName(kind string) string {
	if e := lookup(kind); e != nil {
		return e.Name
	}
	return "Custom service"
}

// reviewed returns the tool definitions Mewla has reviewed for the service.
func (e *entry) reviewed() []Tool {
	if e == nil {
		return nil
	}
	switch {
	case e.Google != nil:
		return googleReviewedTools(e.ID)
	case e.Tools != nil:
		return e.Tools()
	}
	return nil
}

// official reports whether tools of this account are judged by the service's
// official MCP list: the account signed in over MCP, or was added with a
// credential to a service that only speaks MCP.
func (e *entry) official(r *record) bool {
	return r.Account.AuthMethod == "mcp_oauth" || e != nil && e.TokenMCP && e.MCP != nil
}

func (s *mcpSpec) group(name string) string {
	switch {
	case contains(s.Reads, name):
		return "read"
	case contains(s.Writes, name):
		return "write"
	}
	return ""
}

// requestRule returns what a request to this host carries.
func requestRule(host string) *entry {
	return hostRules[strings.ToLower(host)]
}

// changeScopes lists the scopes a grant must hold before the service's
// changes can be allowed. Google asks for them with the sign-in: they are the
// write scopes the read-only sign-in lacks.
func (e *entry) changeScopes() []string {
	switch {
	case e == nil:
		return nil
	case e.Google != nil:
		read := googleauth.Scopes(e.Google.Service, false)
		var extra []string
		for _, scope := range googleauth.Scopes(e.Google.Service, true) {
			if !contains(read, scope) {
				extra = append(extra, scope)
			}
		}
		return extra
	case e.OAuth != nil && e.OAuth.ChangeScopes != nil:
		return e.OAuth.ChangeScopes()
	}
	return nil
}

// codecFor returns the account's own token codec, or nil when its tokens are
// standard OAuth.
func codecFor(r *record) *tokenCodec {
	e := lookup(r.Account.Integration)
	if e == nil || e.OAuth == nil || e.OAuth.Codec == "" {
		return nil
	}
	codec := codecs[e.OAuth.Codec]
	return &codec
}

// endpointOf is a built-in service's API origin.
func endpointOf(id string) string {
	if e := lookup(id); e != nil {
		return e.Endpoint
	}
	return ""
}

// driverFor returns the code behind an account added with a credential.
func driverFor(r *record) (driver, bool) {
	e := lookup(r.Account.Integration)
	if e == nil || e.Driver == "" {
		return driver{}, false
	}
	return drivers[e.Driver], true
}

// invokeTool runs one granted tool of an account that is not reached over MCP.
func (m *Manager) invokeTool(ctx context.Context, r *record, secret string, t Tool, args map[string]any, capture ...*http.Header) ([]byte, error) {
	d, ok := driverFor(r)
	if !ok || d.invoke == nil {
		return nil, errors.New("this plugin has no tools to call")
	}
	return d.invoke(m, ctx, r, secret, t, args, capture...)
}
