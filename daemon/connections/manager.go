package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

type record struct {
	Account Account           `json:"account"`
	Spec    json.RawMessage   `json:"spec,omitempty"`
	OAuth   *oauthAccount     `json:"oauth,omitempty"`
	Grants  map[string]string `json:"grants"` // grant is bound to the full discovered tool definition
}

type Manager struct {
	mu           sync.Mutex // mutations and invocation are linearized; revoke waits for an in-flight call
	path         string
	vault        modelprofiles.CredentialStore
	records      map[string]*record
	http         *http.Client
	networkOwned bool
	pending      map[string]*oauthFlow
	connectFlows map[string]*connectFlow
	githubToken  func(context.Context) (string, error)
}

func New(dir string) (*Manager, error) {
	vault, err := modelprofiles.NewFileCredentialStore(filepath.Join(dir, "integration-credentials.json"))
	if err != nil {
		return nil, err
	}
	m, err := newManager(filepath.Join(dir, "integrations.json"), vault, newHTTPClient())
	if m != nil {
		m.networkOwned = true
	}
	return m, err
}
func newManager(path string, vault modelprofiles.CredentialStore, client *http.Client) (*Manager, error) {
	m := &Manager{path: path, vault: vault, http: client, records: map[string]*record{}, pending: map[string]*oauthFlow{}, connectFlows: map[string]*connectFlow{}, githubToken: readGitHubToken}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(raw, &m.records); err != nil {
			return nil, errors.New("invalid plugin catalog")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if m.records == nil {
		return nil, errors.New("invalid plugin catalog")
	}
	return m, nil
}
func (m *Manager) save() error {
	records := map[string]*record{}
	for id, r := range m.records {
		if !m.isPending(id) {
			records[id] = r
		}
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".integrations-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), m.path)
	}
	if err != nil {
		return errors.New("plugin catalog could not be saved")
	}
	return nil
}

func cloneAccount(a Account) Account {
	raw, _ := json.Marshal(a)
	var out Account
	_ = json.Unmarshal(raw, &out)
	return out
}
func (m *Manager) projection(r *record) Account {
	a := cloneAccount(r.Account)
	for i := range a.Tools {
		a.Tools[i].Group = toolGroup(r, a.Tools[i])
		a.Tools[i].Allowed = r.Grants[a.Tools[i].Name] == fingerprint(a.Tools[i])
	}
	return a
}

// Handle is shared by the paired native client and the daemon's private local
// control socket. Credentials only enter at setup; execution can select an ID
// but cannot override its endpoint, credential, schema, or policy.
func (m *Manager) Handle(parent context.Context, q Request) (Response, error) {
	if m == nil {
		return Response{}, errors.New("plugins are unavailable")
	}
	raw, _ := json.Marshal(q)
	if len(raw) > 2<<20 {
		return Response{}, errors.New("plugin request is too large")
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return Response{}, safeError(ctx.Err())
	}
	m.expireFlows()
	switch q.Action {
	case "connect_start", "connect_status", "connect_finish", "connect_cancel", "github_preview", "github_import":
		return m.connect(ctx, q)
	case "list":
		accounts := make([]Account, 0, len(m.records))
		for _, r := range m.records {
			if m.isPending(r.Account.ID) {
				continue
			}
			a := m.projection(r)
			a.Tools = nil
			accounts = append(accounts, a)
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
		return Response{Catalog: m.catalog(), Accounts: accounts, OAuthConfigured: m.oauthConfigured()}, nil
	case "oauth_configure":
		return m.configureOAuth(q.Input)
	case "oauth_start":
		return m.startOAuth(ctx, q.Input)
	case "add":
		return m.add(ctx, q.Input)
	case "search":
		matches := make([]Match, 0)
		query := strings.ToLower(strings.TrimSpace(q.Query))
		ids := make([]string, 0, len(m.records))
		for id := range m.records {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			r := m.records[id]
			if !r.Account.Enabled || r.Account.Status == "disconnected" {
				continue
			}
			if q.ID != "" && q.ID != id {
				continue
			}
			for _, t := range m.projection(r).Tools {
				if strings.Contains(strings.ToLower(t.Name+" "+t.Description+" "+r.Account.Integration), query) {
					matches = append(matches, Match{id, r.Account.Name, t.Name, t.Description, t.Allowed})
					if len(matches) == 30 {
						return Response{Matches: matches}, nil
					}
				}
			}
		}
		return Response{Matches: matches}, nil
	}
	r, ok := m.records[q.ID]
	if !ok {
		return Response{}, errors.New("plugin account not found")
	}
	if q.Action == "get" {
		a := m.projection(r)
		return Response{Account: &a}, nil
	}
	if q.Action == "describe" {
		for _, t := range m.projection(r).Tools {
			if t.Name == q.Tool {
				return Response{Tool: &t}, nil
			}
		}
		return Response{}, errors.New("tool not found")
	}
	if q.Action == "invoke" {
		return m.invoke(ctx, r, q)
	}
	before, _ := json.Marshal(r)
	switch q.Action {
	case "enable":
		if r.Account.Status == "disconnected" {
			return Response{}, errors.New("connect this account again")
		}
		r.Account.Enabled = true
	case "disable":
		r.Account.Enabled = false
	case "disconnect":
		// Commit the disabled tombstone before deleting its secret. A crash or vault
		// failure can never restore execution on the next startup.
		r.Account.Enabled = false
		r.Account.Status = "disconnected"
		r.Account.CredentialRemovalPending = true
		r.Grants = map[string]string{}
	case "permissions":
		if !m.setGroup(r, q.Group, q.Allowed) {
			return Response{}, errors.New("this permission group is not available")
		}
	case "policy":
		found := false
		for _, t := range r.Account.Tools {
			if t.Name == q.Tool {
				found = true
				if r.Grants == nil {
					r.Grants = map[string]string{}
				}
				if q.Allowed {
					r.Grants[t.Name] = fingerprint(t)
				} else {
					delete(r.Grants, t.Name)
				}
			}
		}
		if !found {
			return Response{}, errors.New("tool not found")
		}
	case "refresh":
		if r.Account.Status == "disconnected" {
			return Response{}, errors.New("connect this account again")
		}
		secret, err := m.accountToken(ctx, r)
		if err != nil {
			return Response{}, err
		}
		err = m.discover(ctx, r, secret)
		if err != nil {
			r.Account.Status = "error"
			m.event(r, "refresh", err)
			_ = m.save()
			return Response{}, err
		}
	default:
		return Response{}, errors.New("unknown plugin operation")
	}
	if err := m.save(); err != nil {
		_ = json.Unmarshal(before, r)
		return Response{}, err
	}
	if q.Action == "disconnect" {
		if r.OAuth != nil {
			if err := m.revokeOAuth(ctx, r); err != nil {
				return Response{}, err
			}
		}
		if err := m.vault.Delete("integration:" + q.ID); err != nil {
			return Response{}, errors.New("account disconnected; stored credential removal failed, retry disconnect")
		}
		r.Account.CredentialRemovalPending = false
		if err := m.save(); err != nil {
			return Response{}, err
		}
	}
	a := m.projection(r)
	return Response{Account: &a}, nil
}

func (m *Manager) add(ctx context.Context, in *Input) (Response, error) {
	if in == nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 80 {
		return Response{}, errors.New("account name is required (up to 80 characters)")
	}
	if len(m.records) >= 50 {
		return Response{}, errors.New("maximum of 50 accounts reached")
	}
	if len(in.Credential) > 16<<10 || strings.ContainsAny(in.Credential, "\r\n") {
		return Response{}, errors.New("invalid credential")
	}
	name := strings.TrimSpace(in.Name)
	if len(in.Spec) != 0 && in.Integration != "openapi" {
		return Response{}, errors.New("only OpenAPI plugins accept a document")
	}
	// The complete document is persisted for dispatch, including fields that do
	// not become tool schemas. Never retain a copied bearer token in that file.
	if in.Credential != "" && len(in.Spec) != 0 {
		escaped, _ := json.Marshal(in.Credential)
		if bytes.Contains(in.Spec, []byte(in.Credential)) || bytes.Contains(in.Spec, escaped[1:len(escaped)-1]) {
			return Response{}, errors.New("remove the account credential from the OpenAPI document")
		}
	}
	for _, r := range m.records {
		if r.Account.Name == name && r.Account.Integration == in.Integration && r.Account.Status != "disconnected" {
			return Response{}, errors.New("choose a unique account name for this plugin")
		}
	}
	endpoint := in.Endpoint
	switch in.Integration {
	case "google":
		return Response{}, errors.New("Google uses browser authorization; start OAuth for this account")
	case "github":
		endpoint = "https://api.github.com"
	case "notion":
		endpoint = "https://api.notion.com/v1"
	case "slack":
		endpoint = "https://slack.com/api"
	case "linear":
		endpoint = "https://mcp.linear.app/mcp"
	case "mcp", "openapi":
	default:
		return Response{}, errors.New("unknown plugin")
	}
	if len(in.TrustedNetworks) > 0 && in.Integration != "mcp" && in.Integration != "openapi" {
		return Response{}, errors.New("internal network trust is only supported for custom plugins")
	}
	if _, err := trustedEndpointURL(endpoint, in.TrustedNetworks); err != nil {
		return Response{}, err
	}
	if (in.Integration == "github" || in.Integration == "notion" || in.Integration == "slack" || in.Integration == "linear") && strings.TrimSpace(in.Credential) == "" {
		return Response{}, errors.New("connect an authorized account first")
	}
	r := &record{Account: Account{ID: uuid.NewString(), Integration: in.Integration, Name: name, Endpoint: endpoint, TrustedNetworks: append([]string(nil), in.TrustedNetworks...), Enabled: true, History: []Event{}}, Spec: in.Spec, Grants: map[string]string{}}
	if err := m.discover(ctx, r, in.Credential); err != nil {
		return Response{}, err
	}
	for _, t := range r.Account.Tools {
		if t.Read {
			r.Grants[t.Name] = fingerprint(t)
		}
	}
	public, _ := json.Marshal(r.Account)
	if in.Credential != "" && bytes.Contains(public, []byte(in.Credential)) {
		return Response{}, errors.New("service echoed credential in account metadata")
	}
	ref := "integration:" + r.Account.ID
	if in.Credential != "" {
		if err := m.vault.Set(ref, in.Credential); err != nil {
			return Response{}, errors.New("credential could not be stored")
		}
	}
	m.records[r.Account.ID] = r
	if err := m.save(); err != nil {
		delete(m.records, r.Account.ID)
		_ = m.vault.Delete(ref)
		return Response{}, err
	}
	a := m.projection(r)
	return Response{Account: &a}, nil
}

func (m *Manager) event(r *record, tool string, err error) {
	e := Event{At: time.Now().UTC(), Tool: tool, Status: "success"}
	if err != nil {
		e.Status = "error"
		e.Message = err.Error()
	}
	r.Account.History = append([]Event{e}, r.Account.History...)
	if len(r.Account.History) > 20 {
		r.Account.History = r.Account.History[:20]
	}
}
func (m *Manager) invoke(ctx context.Context, r *record, q Request) (Response, error) {
	if !r.Account.Enabled || r.Account.Status == "disconnected" {
		return Response{}, errors.New("plugin account is disabled or disconnected")
	}
	if len(q.Arguments) == 0 {
		q.Arguments = json.RawMessage(`{}`)
	}
	if len(q.Arguments) > MaxInputBytes {
		return Response{}, errors.New("tool input exceeds 64 KiB")
	}
	var t *Tool
	for _, tool := range r.Account.Tools {
		if tool.Name == q.Tool {
			v := tool
			t = &v
			break
		}
	}
	if t == nil {
		return Response{}, errors.New("tool not found")
	}
	if r.Grants[t.Name] != fingerprint(*t) {
		return Response{}, errors.New("tool is not authorized; enable this tool for this account in Plugins")
	}
	var schema jsonschema.Schema
	if json.Unmarshal(t.InputSchema, &schema) != nil {
		return Response{}, errors.New("tool schema is invalid")
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return Response{}, errors.New("tool schema cannot be resolved")
	}
	var args map[string]any
	if json.Unmarshal(q.Arguments, &args) != nil || args == nil {
		return Response{}, errors.New("tool arguments must be a JSON object")
	}
	if err = resolved.Validate(args); err != nil {
		return Response{}, errors.New("arguments do not match the tool schema; describe the tool first")
	}
	secret, err := m.accountToken(ctx, r)
	ok := secret != ""
	if err != nil {
		return Response{}, err
	}
	if !ok && (r.Account.Integration == "github" || r.Account.Integration == "notion" || r.Account.Integration == "slack" || r.Account.Integration == "linear") {
		return Response{}, errAuth
	}
	var result []byte
	if r.Account.Integration == "mcp" || r.Account.Integration == "linear" || r.Account.AuthMethod == "mcp_oauth" {
		result, err = m.invokeMCP(ctx, r, secret, *t, args)
	} else {
		if r.Account.Integration == "google" {
			result, err = m.invokeGoogle(ctx, r, secret, *t, args)
		} else {
			result, err = m.invokeHTTP(ctx, r, secret, *t, args)
		}
	}
	if ctx.Err() != nil {
		err = safeError(ctx.Err())
	}
	if err == nil && !json.Valid(result) {
		err = errors.New("integration returned invalid JSON")
	}
	if err == nil && len(result) > MaxResultBytes {
		err = errors.New("integration result exceeds 1 MiB")
	}
	if err == nil {
		for _, value := range m.sensitiveValues(r, secret) {
			if value == "" {
				continue
			}
			result = bytes.ReplaceAll(result, []byte(value), []byte("[REDACTED]"))
			escaped, _ := json.Marshal(value)
			if len(escaped) > 2 {
				result = bytes.ReplaceAll(result, escaped[1:len(escaped)-1], []byte("[REDACTED]"))
			}
		}
	}
	if errors.Is(err, errAuth) {
		r.Account.Status = "authorization_required"
	} else if err != nil {
		r.Account.Status = "error"
	} else {
		r.Account.Status = "connected"
		now := time.Now().UTC()
		r.Account.VerifiedAt = &now
	}
	m.event(r, q.Tool, err)
	if saveErr := m.save(); saveErr != nil {
		return Response{}, errors.New("call finished but history could not be saved; inspect before retrying")
	}
	if err != nil {
		return Response{}, err
	}
	return Response{Result: result}, nil
}
