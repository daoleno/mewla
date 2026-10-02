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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	SetupURL    string `json:"setup_url"`
	Description string `json:"description"`
}

func Catalog() []Integration {
	return []Integration{
		{"github", "GitHub", true, "https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps", "Repositories, issues and pull requests"},
		{"notion", "Notion", true, "https://www.notion.so/profile/integrations", "Pages, search and workspace content"},
		{"google", "Google Workspace", true, "https://developers.google.com/workspace/guides/configure-oauth-consent", "Files, email and calendar events"},
		{"slack", "Slack", true, "https://api.slack.com/apps", "Channels, conversations and messages"},
		{"linear", "Linear", true, "https://linear.app/settings/account/security", "Issues, projects and team activity"},
		{"mcp", "Remote MCP", true, "https://modelcontextprotocol.io", "Discover tools from a remote MCP server"},
		{"openapi", "OpenAPI", true, "https://spec.openapis.org/oas/v3.0.3", "Discover operations from an OpenAPI 3 document"},
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
