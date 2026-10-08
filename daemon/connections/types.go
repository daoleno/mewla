// Package connections owns server-local integration accounts, capabilities and execution.
package connections

import (
	"encoding/json"
	"time"
)

const MaxResultBytes = 1 << 20
const MaxInputBytes = 64 << 10
const MaxTools = 200

type Integration struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Available         bool   `json:"available"`
	SetupURL          string `json:"setup_url"`
	Description       string `json:"description"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

func Catalog() []Integration {
	return []Integration{
		{ID: "github", Name: "GitHub", Available: true, SetupURL: "https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps", Description: "Repositories, issues and pull requests"},
		{ID: "notion", Name: "Notion", Available: true, SetupURL: "https://www.notion.so/profile/integrations", Description: "Pages, search and workspace content"},
		{ID: "google", Name: "Google Workspace", Available: true, SetupURL: "https://developers.google.com/workspace/guides/configure-oauth-consent", Description: "Files, email and calendar events"},
		{ID: "slack", Name: "Slack", Available: true, SetupURL: "https://api.slack.com/apps", Description: "Channels, conversations and messages"},
		{ID: "linear", Name: "Linear", Available: true, SetupURL: "https://linear.app/settings/account/security", Description: "Issues, projects and team activity"},
		{ID: "mcp", Name: "Remote MCP", Available: true, SetupURL: "https://modelcontextprotocol.io", Description: "Discover tools from a remote MCP server"},
		{ID: "openapi", Name: "OpenAPI", Available: true, SetupURL: "https://spec.openapis.org/oas/v3.0.3", Description: "Discover operations from an OpenAPI 3 document"},
	}
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Allowed     bool            `json:"allowed"`
	Group       string          `json:"group,omitempty"`
	// HTTP routing is private; never accepted from an execution request.
	Method string `json:"-"`
	Path   string `json:"-"`
	Read   bool   `json:"-"` // reviewed built-in semantics, never inferred from remote annotations
}

type Event struct {
	At      time.Time `json:"at"`
	Tool    string    `json:"tool"`
	Status  string    `json:"status"`
	Message string    `json:"message,omitempty"`
}

type Account struct {
	ID                       string     `json:"id"`
	Integration              string     `json:"integration"`
	Name                     string     `json:"name"`
	Identity                 string     `json:"identity"`
	Endpoint                 string     `json:"endpoint,omitempty"`
	TrustedNetworks          []string   `json:"trusted_networks,omitempty"`
	AuthMethod               string     `json:"auth_method,omitempty"`
	Scopes                   []string   `json:"scopes,omitempty"`
	Enabled                  bool       `json:"enabled"`
	CredentialRemovalPending bool       `json:"credential_removal_pending,omitempty"`
	Status                   string     `json:"status"`
	VerifiedAt               *time.Time `json:"verified_at,omitempty"`
	Tools                    []Tool     `json:"tools"`
	History                  []Event    `json:"history"`
}

type Input struct {
	Integration     string             `json:"integration"`
	Name            string             `json:"name"`
	Endpoint        string             `json:"endpoint,omitempty"`
	TrustedNetworks []string           `json:"trusted_networks,omitempty"`
	Credential      string             `json:"credential,omitempty"`
	Spec            json.RawMessage    `json:"spec,omitempty"`
	OAuthClient     *OAuthClientConfig `json:"oauth_client,omitempty"`
	Mobile          bool               `json:"mobile,omitempty"`
	AllowWrites     bool               `json:"allow_writes,omitempty"`
	// WebOrigin is the web UI's own origin. connect_start returns its browser
	// there instead of to the native app, if the daemon serves the web UI on it.
	WebOrigin string `json:"web_origin,omitempty"`
	callback  string // connect_start's return target; never decoded
}

type Request struct {
	FlowID    string          `json:"flow_id,omitempty"`
	Callback  string          `json:"callback,omitempty"`
	Group     string          `json:"group,omitempty"`
	Action    string          `json:"action"`
	ID        string          `json:"id,omitempty"`
	Input     *Input          `json:"input,omitempty"`
	Query     string          `json:"query,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Allowed   bool            `json:"allowed,omitempty"`
}

type Match struct {
	ConnectionID string `json:"connection_id"`
	Account      string `json:"account"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Allowed      bool   `json:"allowed"`
}

type Response struct {
	Flow             *ConnectFlow    `json:"flow,omitempty"`
	Catalog          []Integration   `json:"catalog,omitempty"`
	Accounts         []Account       `json:"accounts,omitempty"`
	Account          *Account        `json:"account,omitempty"`
	Matches          []Match         `json:"matches,omitempty"`
	Tool             *Tool           `json:"tool,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	AuthorizationURL string          `json:"authorization_url,omitempty"`
	OAuthConfigured  []string        `json:"oauth_configured"`
}
