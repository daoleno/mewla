package connections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Tailscale's API is reached with an OAuth client: a client ID and secret that
// don't expire and mint one-hour access tokens (client credentials grant). The
// client belongs to one tailnet, so there is nothing for the person to choose:
// the credential names its tailnet, and "-" stands for it in every path.
const (
	tailscaleOrigin    = "https://api.tailscale.com"
	tailscaleAPIPath   = "/api/v2"
	tailscaleTokenPath = "/api/v2/oauth/token"
	// TailscaleCredentialsURL is the admin console page where an OAuth client is
	// created, for a person who creates it by hand.
	TailscaleCredentialsURL = "https://login.tailscale.com/admin/settings/trust-credentials"
	// tailscaleClientName is how the client Mewla creates is named in the
	// admin console.
	tailscaleClientName = "Mewla"
)

// tailscaleScopes are what a client created for Mewla is given. Reads cover
// the whole tailnet; the write scopes are the ones needed for everyday
// management. Mewla still allows a change only with the person's say-so for
// the task.
//
//   - all:read: every read endpoint, including ones Tailscale adds later.
//   - devices:core: delete, rename, expire, authorize and tag devices. Tailscale
//     requires a tag on a credential with this scope.
//   - auth_keys: create and revoke auth keys. Also requires a tag.
//   - policy_file: read and edit the access policy. Tailscale requires
//     devices:posture_attributes (write) and devices:core:read with it.
//   - devices:posture_attributes: that dependency, device posture attributes.
//   - dns: read and edit DNS.
//
// Not requested: users, billing-adjacent settings, webhooks, log streaming,
// trust credentials themselves and "all".
var tailscaleScopes = []string{"all:read", "devices:core", "auth_keys", "policy_file", "devices:posture_attributes", "dns"}

// TailscaleScopes is the scope list the browser connect asks for.
func TailscaleScopes() []string { return append([]string(nil), tailscaleScopes...) }

var tsClientSecret = regexp.MustCompile(`^tskey-client-([A-Za-z0-9]+)-[A-Za-z0-9]+$`)

// parseTailscaleCredential accepts what a person has on the Credential created
// page: the client secret (tskey-client-<ID>-<random>, which carries the ID), or
// the ID and secret together as "ID:SECRET" or "ID SECRET". It returns the
// stored form, "ID:SECRET".
func parseTailscaleCredential(raw string) (id, secret, stored string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 512 || strings.ContainsAny(raw, "\r\n\"") {
		return "", "", "", errors.New("Paste the client secret from Tailscale.")
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ':' || r == ' ' || r == '\t' || r == ',' })
	switch len(fields) {
	case 1:
		secret = fields[0]
		m := tsClientSecret.FindStringSubmatch(secret)
		if m == nil {
			return "", "", "", errors.New("That is not a Tailscale client secret. It starts with tskey-client-.")
		}
		id = m[1]
	case 2:
		id, secret = fields[0], fields[1]
		if strings.HasPrefix(id, "tskey-client-") {
			id, secret = secret, id
		}
	default:
		return "", "", "", errors.New("Paste the client secret from Tailscale.")
	}
	if !strings.HasPrefix(secret, "tskey-client-") || id == "" || strings.ContainsAny(id, "/?#&= ") {
		return "", "", "", errors.New("That is not a Tailscale client ID and secret.")
	}
	return id, secret, id + ":" + secret, nil
}

func splitTailscaleCredential(stored string) (id, secret string, ok bool) {
	id, secret, ok = strings.Cut(stored, ":")
	return id, secret, ok && id != "" && secret != ""
}

type tsToken struct {
	value   string
	scope   string
	expires time.Time
}

var tailscaleTokens = struct {
	sync.Mutex
	byKey map[string]tsToken
}{byKey: map[string]tsToken{}}

func tsTokenKey(endpoint, stored string) string {
	sum := sha256.Sum256([]byte(endpoint + "\x00" + stored))
	return hex.EncodeToString(sum[:])
}

// tailscaleToken returns a live access token for the client credential,
// minting one when none is held or it is close to expiring.
func (m *Manager) tailscaleToken(ctx context.Context, r *record, stored string) (tsToken, error) {
	key := tsTokenKey(r.Account.Endpoint, stored)
	tailscaleTokens.Lock()
	held, ok := tailscaleTokens.byKey[key]
	tailscaleTokens.Unlock()
	if ok && time.Until(held.expires) > 2*time.Minute {
		return held, nil
	}
	id, secret, valid := splitTailscaleCredential(stored)
	if !valid {
		return tsToken{}, errAuth
	}
	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	form := url.Values{"client_id": {id}, "client_secret": {secret}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.Account.Endpoint, "/")+tailscaleTokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return tsToken{}, errors.New("invalid request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mewla-Integrations")
	resp, err := m.oauthHTTP(r).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return tsToken{}, safeError(ctx.Err())
		}
		return tsToken{}, errNetwork
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return tsToken{}, errAuth
	case resp.StatusCode != http.StatusOK:
		return tsToken{}, StatusError{Status: resp.StatusCode}
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil || data.AccessToken == "" || len(data.AccessToken) > 4096 || strings.ContainsAny(data.AccessToken, "\r\n") {
		return tsToken{}, errors.New("Tailscale returned an invalid access token")
	}
	lifetime := time.Duration(data.ExpiresIn) * time.Second
	if lifetime <= 0 || lifetime > 24*time.Hour {
		lifetime = time.Hour
	}
	token := tsToken{value: data.AccessToken, scope: data.Scope, expires: time.Now().Add(lifetime)}
	tailscaleTokens.Lock()
	tailscaleTokens.byKey[key] = token
	tailscaleTokens.Unlock()
	return token, nil
}

func tailscaleTools() []Tool {
	text := func(desc string) map[string]any { s := str(); s["description"] = desc; return s }
	operation := text("An operation name from search, such as listTailnetDevices.")
	path := map[string]any{"type": "object", "description": "Path parameters by name. tailnet defaults to this credential's own tailnet; leave it out.", "additionalProperties": map[string]any{"type": []string{"string", "integer"}}}
	query := map[string]any{"type": "object", "description": "Query parameters by name.", "additionalProperties": true}
	read := map[string]any{"operation": operation, "path": path, "query": query}
	write := map[string]any{"operation": operation, "path": path, "query": query,
		"body":      map[string]any{"description": "The JSON request body the operation takes."},
		"authorize": map[string]any{"type": "boolean", "description": "True only with explicit user authorization for this task."}}
	return []Tool{
		hlTool("search", "Search Tailscale's API reference for the operation that does something: devices, auth keys, access policy, DNS, users, routes, webhooks, logs. Returns each operation's name, method, path, parameters and body fields. Pass an operation name as the query to see it in full.", true, map[string]any{
			"query": text("Words to look for, or an operation name."),
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 30},
		}, "query"),
		hlTool("read", "Call a read operation (GET) from search, in this credential's tailnet. For deleting, renaming, changing policy or DNS, or creating keys, use execute.", true, read, "operation"),
		hlTool("execute", "Call any operation from search, including changes: delete, rename or expire a device, set access policy or DNS, create or revoke auth keys. Needs enabled changes and the user's authorization for this task; pass authorize=true only then. Never retried.", false, write, "operation", "authorize"),
	}
}

func discoverTailscale(m *Manager, ctx context.Context, r *record, stored string) ([]Tool, error) {
	token, err := m.tailscaleToken(ctx, r, stored)
	if err != nil {
		return nil, err
	}
	r.Account.AuthMethod = "client_credentials"
	r.Account.Scopes = strings.Fields(token.scope)
	r.Account.Identity = "Tailscale"
	// The tailnet's name is in the suffix of its devices' names. A credential
	// that can't list devices still connects.
	if hasAnyScope(r.Account.Scopes, "all", "all:read", "devices:core", "devices:core:read") {
		raw, err := m.request(ctx, r, r.Account.Endpoint, token.value, http.MethodGet, tailscaleAPIPath+"/tailnet/-/devices", nil)
		if errors.Is(err, errAuth) {
			return nil, err
		}
		if err == nil {
			if name := tailnetName(raw); name != "" {
				r.Account.Identity = name
			}
		}
	}
	return tailscaleTools(), nil
}

func hasAnyScope(have []string, want ...string) bool {
	for _, w := range want {
		if contains(have, w) {
			return true
		}
	}
	return false
}

// tailnetName reads the tailnet's domain from the first device's name, which
// is "<host>.<tailnet>.ts.net".
func tailnetName(devices []byte) string {
	var list struct {
		Devices []struct {
			Name string `json:"name"`
		} `json:"devices"`
	}
	if json.Unmarshal(devices, &list) != nil {
		return ""
	}
	for _, d := range list.Devices {
		if _, domain, ok := strings.Cut(d.Name, "."); ok && strings.HasSuffix(domain, ".ts.net") {
			return domain
		}
	}
	return ""
}

func invokeTailscale(m *Manager, ctx context.Context, r *record, stored string, t Tool, args map[string]any, _ ...*http.Header) ([]byte, error) {
	if stored == "" {
		return nil, errAuth
	}
	if t.Name == "search" {
		return m.tailscaleSearch(ctx, r, args)
	}
	name, _ := args["operation"].(string)
	index, err := m.tailscaleIndex(ctx, r, false)
	if err != nil {
		return nil, err
	}
	op := index.ops[name]
	if op == nil {
		return nil, callRefused("Unknown operation " + cutRunes(name, 60) + ". Use search to find the operation name.")
	}
	switch t.Name {
	case "read":
		if op.Method != http.MethodGet {
			return nil, callRefused(op.ID + " changes things. It needs execute and task-specific user authorization.")
		}
	case "execute":
		if args["authorize"] != true {
			return nil, callRefused("This needs the user's authorization for this task. Ask, then call again with authorize=true.")
		}
	default:
		return nil, errors.New("tool not found")
	}
	path, query, body, err := op.request(args)
	if err != nil {
		return nil, callRefused(err.Error())
	}
	token, err := m.tailscaleToken(ctx, r, stored)
	if err != nil {
		return nil, err
	}
	target := tailscaleAPIPath + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	result, err := m.request(ctx, r, r.Account.Endpoint, token.value, op.Method, target, bytes.NewReader(body))
	var status StatusError
	if errors.As(err, &status) {
		switch status.Status {
		case http.StatusForbidden:
			return nil, callRefused("Tailscale refused " + op.ID + " (HTTP 403). This credential's scopes may not allow it" + scopeHint(op) + ".")
		case http.StatusNotFound:
			return nil, callRefused("Tailscale found nothing for " + op.ID + " (HTTP 404). Check the identifiers.")
		case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusConflict, http.StatusPreconditionFailed:
			return nil, callRefused(fmt.Sprintf("Tailscale rejected %s (HTTP %d). Check the arguments against search.", op.ID, status.Status))
		}
	}
	if errors.Is(err, errAuth) {
		// A 401 on an API call after a fresh token means the client was revoked.
		tailscaleTokens.Lock()
		delete(tailscaleTokens.byKey, tsTokenKey(r.Account.Endpoint, stored))
		tailscaleTokens.Unlock()
	}
	return result, err
}

func scopeHint(op *tsOperation) string {
	if op.Scope == "" {
		return ""
	}
	return " (needs " + op.Scope + ")"
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (m *Manager) tailscaleSearch(ctx context.Context, r *record, args map[string]any) ([]byte, error) {
	query, _ := args["query"].(string)
	limit := 15
	if n, ok := args["limit"].(float64); ok {
		limit = int(n)
	}
	index, err := m.tailscaleIndex(ctx, r, false)
	if err != nil {
		return nil, err
	}
	hits := index.search(query, limit)
	detail := len(hits) == 1
	out := make([]map[string]any, 0, len(hits))
	for _, op := range hits {
		out = append(out, op.summary(detail))
	}
	return json.Marshal(map[string]any{"operations": out, "tailnet": "Leave the tailnet path parameter out: it is this credential's own tailnet."})
}

// request builds the path, query and body of a call from the caller's
// arguments. Nothing is taken from the caller but values: the method and the
// path come from the reference, so a call can only reach a documented
// operation of Tailscale's API.
func (o *tsOperation) request(args map[string]any) (path string, query url.Values, body []byte, err error) {
	given, _ := args["path"].(map[string]any)
	path = o.Path
	for _, p := range o.Params {
		if p.In != "path" {
			continue
		}
		value, ok := given[p.Name]
		text := ""
		switch v := value.(type) {
		case string:
			text = v
		case float64:
			text = fmt.Sprint(int64(v))
		}
		if !ok || text == "" {
			if p.Name != "tailnet" {
				return "", nil, nil, fmt.Errorf("%s needs the path parameter %s", o.ID, p.Name)
			}
			text = "-"
		}
		if strings.ContainsAny(text, "/\\%?#") || text == "." || text == ".." {
			return "", nil, nil, errors.New("path parameters must be single path segments")
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(text))
	}
	for name := range given {
		if !o.has("path", name) {
			return "", nil, nil, fmt.Errorf("%s has no path parameter %s", o.ID, name)
		}
	}
	if strings.ContainsAny(path, "{}") {
		return "", nil, nil, errors.New("missing path parameter")
	}
	query = url.Values{}
	asked, _ := args["query"].(map[string]any)
	names := make([]string, 0, len(asked))
	for name := range asked {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !o.has("query", name) {
			return "", nil, nil, fmt.Errorf("%s has no query parameter %s", o.ID, name)
		}
		switch v := asked[name].(type) {
		case []any:
			for _, item := range v {
				query.Add(name, fmt.Sprint(item))
			}
		case map[string]any, nil:
			return "", nil, nil, fmt.Errorf("query parameter %s must be a value or a list", name)
		default:
			query.Set(name, fmt.Sprint(v))
		}
	}
	for _, p := range o.Params {
		if p.In == "query" && p.Required && query.Get(p.Name) == "" {
			return "", nil, nil, fmt.Errorf("%s needs the query parameter %s", o.ID, p.Name)
		}
	}
	if b, ok := args["body"]; ok && b != nil {
		if o.Body == nil && o.Method == http.MethodGet {
			return "", nil, nil, errors.New("this operation takes no body")
		}
		if body, err = json.Marshal(b); err != nil {
			return "", nil, nil, errors.New("body must be JSON")
		}
	} else if o.BodyNeeded {
		return "", nil, nil, fmt.Errorf("%s needs a body; search shows its fields", o.ID)
	}
	return path, query, body, nil
}

func (o *tsOperation) has(in, name string) bool {
	for _, p := range o.Params {
		if p.In == in && p.Name == name {
			return true
		}
	}
	return false
}

// addTailscale connects a Tailscale OAuth client from its ID and secret: the
// person pasted it, or the browser connect read it from the admin console. The
// tailnet it belongs to names the account, and connecting a client of a tailnet
// that is already connected replaces that account's credential.
func addTailscale(m *Manager, ctx context.Context, in *Input) (Response, error) {
	if in == nil {
		return Response{}, errors.New("Paste the client secret from Tailscale.")
	}
	_, _, stored, err := parseTailscaleCredential(in.Credential)
	if err != nil {
		return Response{}, err
	}
	r, err := m.verifyTailscale(ctx, stored)
	if err != nil {
		return Response{}, err
	}
	a, err := m.saveTailscale(r, stored, in.AllowWrites)
	if err != nil {
		return Response{}, err
	}
	return Response{Account: &a}, nil
}

// verifyTailscale makes a new account record for a credential, proving it
// works by minting a token.
func (m *Manager) verifyTailscale(ctx context.Context, stored string) (*record, error) {
	e := lookup("tailscale")
	r := &record{Account: Account{ID: uuid.NewString(), Integration: "tailscale", Name: e.Name, Endpoint: e.Endpoint, Enabled: true, History: []Event{}}, Grants: map[string]string{}}
	if err := m.discover(ctx, r, stored); err != nil {
		if errors.Is(err, errAuth) {
			return nil, errors.New("Tailscale did not accept this client ID and secret.")
		}
		return nil, err
	}
	return r, nil
}

func (m *Manager) saveTailscale(r *record, stored string, writes bool) (Account, error) {
	if len(m.records) >= 50 {
		return Account{}, errors.New("maximum of 50 accounts reached")
	}
	if r.Account.Identity != "" && r.Account.Identity != "Tailscale" {
		r.Account.Name = r.Account.Identity
	}
	var previous *record
	for _, existing := range m.records {
		if existing.Account.Integration == "tailscale" && existing.Account.Status != "disconnected" && existing.Account.Identity == r.Account.Identity {
			previous = existing
		}
	}
	if previous == nil {
		for _, existing := range m.records {
			if existing.Account.Integration == "tailscale" && existing.Account.Status != "disconnected" && existing.Account.Name == r.Account.Name {
				r.Account.Name += " (" + r.Account.ID[:4] + ")"
			}
		}
	} else {
		keepAccount(r, previous)
		r.Account.ID = previous.Account.ID
	}
	m.setGroup(r, "read", true)
	if writes {
		m.setGroup(r, "write", true)
	}
	ref := "integration:" + r.Account.ID
	oldRaw, oldOK, err := m.vault.Get(ref)
	if err != nil {
		return Account{}, errors.New("credential store unavailable")
	}
	if err := m.vault.Set(ref, stored); err != nil {
		return Account{}, errors.New("credential could not be stored")
	}
	old := m.records[r.Account.ID]
	m.records[r.Account.ID] = r
	if err := m.save(); err != nil {
		if old != nil {
			m.records[r.Account.ID] = old
		} else {
			delete(m.records, r.Account.ID)
		}
		if oldOK {
			_ = m.vault.Set(ref, oldRaw)
		} else {
			_ = m.vault.Delete(ref)
		}
		return Account{}, err
	}
	return m.projection(r), nil
}

// tailscaleSecrets are the values of a Tailscale credential that must never
// appear in a result or an account's metadata: the client secret and the
// access token minted from it.
func tailscaleSecrets(r *record, stored string) []string {
	out := []string{}
	if _, secret, ok := splitTailscaleCredential(stored); ok {
		out = append(out, secret)
	}
	tailscaleTokens.Lock()
	if token, ok := tailscaleTokens.byKey[tsTokenKey(r.Account.Endpoint, stored)]; ok {
		out = append(out, token.value)
	}
	tailscaleTokens.Unlock()
	return out
}
