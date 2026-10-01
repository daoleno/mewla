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
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

func fingerprint(t Tool) string {
	raw, _ := json.Marshal(struct {
		Name, Description string
		Schema            json.RawMessage
	}{t.Name, t.Description, t.InputSchema})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func object(properties map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
func str() map[string]any { return map[string]any{"type": "string", "minLength": 1, "maxLength": 4096} }
func pageSize() map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
}
func builtin(name, desc, method, path string, read bool, params, query, body map[string]any) Tool {
	p := map[string]any{}
	required := []string{}
	if params != nil {
		p["path"] = params
		required = append(required, "path")
	}
	if query != nil {
		p["query"] = query
		if _, ok := query["required"]; ok {
			required = append(required, "query")
		}
	}
	if body != nil {
		p["body"] = body
		required = append(required, "body")
	}
	schema, _ := json.Marshal(object(p, required...))
	return Tool{Name: name, Description: desc, InputSchema: schema, Method: method, Path: path, Read: read}
}
func builtinTools(kind string) []Tool {
	if kind == "slack" {
		return slackTools()
	}
	repo := object(map[string]any{"owner": str(), "repo": str()}, "owner", "repo")
	issue := object(map[string]any{"owner": str(), "repo": str(), "number": map[string]any{"type": "integer", "minimum": 1}}, "owner", "repo", "number")
	page := object(map[string]any{"id": str()}, "id")
	pagination := object(map[string]any{"per_page": pageSize(), "page": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000}})
	if kind == "github" {
		return []Tool{
			builtin("get_me", "Read the authenticated GitHub account", "GET", "/user", true, nil, nil, nil),
			builtin("list_repositories", "List repositories visible to this account (one page)", "GET", "/user/repos", true, nil, pagination, nil),
			builtin("get_repository", "Read repository details", "GET", "/repos/{owner}/{repo}", true, repo, nil, nil),
			builtin("list_issues", "List repository issues and pull requests (one page)", "GET", "/repos/{owner}/{repo}/issues", true, repo, pagination, nil),
			builtin("get_issue", "Read an issue or pull request", "GET", "/repos/{owner}/{repo}/issues/{number}", true, issue, nil, nil),
			builtin("create_issue", "Create a repository issue", "POST", "/repos/{owner}/{repo}/issues", false, repo, nil, object(map[string]any{"title": str(), "body": str()}, "title")),
			builtin("comment_on_issue", "Post a comment on an issue or pull request", "POST", "/repos/{owner}/{repo}/issues/{number}/comments", false, issue, nil, object(map[string]any{"body": str()}, "body")),
		}
	}
	return []Tool{
		builtin("get_me", "Read this Notion integration and workspace identity", "GET", "/users/me", true, nil, nil, nil),
		builtin("search", "Search pages shared with this Notion connection (one page)", "POST", "/search", true, nil, nil, object(map[string]any{"query": str(), "start_cursor": str(), "page_size": pageSize()})),
		builtin("get_page", "Read a shared Notion page", "GET", "/pages/{id}", true, page, nil, nil),
		builtin("get_block", "Read a shared Notion block", "GET", "/blocks/{id}", true, page, nil, nil),
		builtin("list_block_children", "Read blocks in a page (one page)", "GET", "/blocks/{id}/children", true, page, object(map[string]any{"start_cursor": str(), "page_size": pageSize()}), nil),
		builtin("update_page", "Update page properties or archive state", "PATCH", "/pages/{id}", false, page, nil, object(map[string]any{"properties": map[string]any{"type": "object"}, "archived": map[string]any{"type": "boolean"}})),
		builtin("append_blocks", "Append content blocks to a shared page", "PATCH", "/blocks/{id}/children", false, page, nil, object(map[string]any{"children": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "object"}}}, "children")),
	}
}

func (m *Manager) discover(ctx context.Context, r *record, secret string) error {
	candidate := *r
	candidate.Account = cloneAccount(r.Account)
	if err := m.discoverUnchecked(ctx, &candidate, secret); err != nil {
		return err
	}
	raw, _ := json.Marshal(candidate.Account)
	for _, value := range m.sensitiveValues(r, secret) {
		escaped, _ := json.Marshal(value)
		if value != "" && (bytes.Contains(raw, []byte(value)) || bytes.Contains(raw, escaped[1:len(escaped)-1])) {
			return errors.New("service echoed credential in account metadata")
		}
	}
	r.Account = candidate.Account
	return nil
}
func (m *Manager) discoverUnchecked(ctx context.Context, r *record, secret string) error {
	var tools []Tool
	if r.Account.AuthMethod == "mcp_oauth" {
		var err error
		tools, err = m.discoverMCP(ctx, r, secret)
		if err != nil {
			return err
		}
		r.Account.Identity = "MCP account · identity not provided by server"
	} else {
		switch r.Account.Integration {
		case "google":
			if err := m.discoverGoogle(ctx, r, secret); err != nil {
				return err
			}
			tools = r.Account.Tools
		case "github", "notion":
			raw, err := m.request(ctx, r, r.Account.Endpoint, secret, "GET", map[string]string{"github": "/user", "notion": "/users/me"}[r.Account.Integration], nil)
			if err != nil {
				return err
			}
			var user struct {
				Login string `json:"login"`
				Name  string `json:"name"`
				ID    any    `json:"id"`
				Bot   struct {
					WorkspaceName string `json:"workspace_name"`
				} `json:"bot"`
			}
			if json.Unmarshal(raw, &user) != nil || user.ID == nil {
				return errors.New("service did not return an account identity")
			}
			r.Account.Identity = user.Login
			if r.Account.Identity == "" {
				r.Account.Identity = user.Bot.WorkspaceName
			}
			if r.Account.Identity == "" {
				r.Account.Identity = user.Name
			}
			if r.Account.Identity == "" {
				r.Account.Identity = fmt.Sprint(user.ID)
			}
			tools = builtinTools(r.Account.Integration)
		case "slack":
			var headers http.Header
			raw, err := m.request(ctx, r, r.Account.Endpoint, secret, "GET", "/auth.test", nil, &headers)
			if err != nil {
				return err
			}
			var identity struct {
				Team   string `json:"team"`
				User   string `json:"user"`
				UserID string `json:"user_id"`
				BotID  string `json:"bot_id"`
			}
			if json.Unmarshal(raw, &identity) != nil || identity.UserID == "" {
				return errors.New("Slack did not return an account identity")
			}
			r.Account.Identity = identity.Team + " · " + identity.User
			granted := map[string]bool{}
			for _, scope := range strings.Split(headers.Get("X-OAuth-Scopes"), ",") {
				granted[strings.TrimSpace(scope)] = true
			}
			for _, tool := range slackTools() {
				allowed := tool.Name == "get_me"
				for _, scope := range slackToolScopes[tool.Name] {
					if granted[scope] {
						allowed = true
					}
				}
				if tool.Name == "search_messages" && identity.BotID != "" {
					allowed = false
				}
				if allowed {
					tools = append(tools, tool)
				}
			}
		case "openapi":
			var err error
			tools, err = openAPITools(r.Spec)
			if err != nil {
				return err
			}
			// A schema is not proof of account authorization. The first permitted call
			// verifies the remote endpoint; never label an uploaded spec connected.
			r.Account.Tools = tools
			r.Account.Identity = "Account identity unavailable"
			if r.Account.VerifiedAt == nil {
				r.Account.Status = "configured"
			}
			return nil
		case "mcp", "linear":
			var err error
			tools, err = m.discoverMCP(ctx, r, secret)
			if err != nil {
				return err
			}
			r.Account.Identity = "MCP account · identity not provided by server"
		}
	}
	if len(tools) > MaxTools {
		return errors.New("plugin exposes more than 200 tools; use a narrower endpoint")
	}
	for _, t := range tools {
		if len(t.InputSchema) > 32<<10 {
			return errors.New("tool schema exceeds 32 KiB")
		}
	}
	r.Account.Tools = tools
	r.Account.Status = "connected"
	now := time.Now().UTC()
	r.Account.VerifiedAt = &now
	return nil
}

func openAPITools(spec json.RawMessage) ([]Tool, error) {
	if len(spec) == 0 || len(spec) > 1<<20 {
		return nil, errors.New("provide an OpenAPI 3.0 JSON document up to 1 MiB")
	}
	// Inline local references only, with a depth limit. Remote references must
	// never let an uploaded document read host files or make extra requests.
	var doc any
	if json.Unmarshal(spec, &doc) != nil {
		return nil, errors.New("invalid OpenAPI JSON")
	}
	expanded, err := expandRefs(doc, doc, 0)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(expanded)
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	api, err := loader.LoadFromData(raw)
	if err != nil || api.Paths == nil || !strings.HasPrefix(api.OpenAPI, "3.0.") {
		return nil, errors.New("supported document format is OpenAPI 3.0 with local, nonrecursive references")
	}
	tools := []Tool{}
	names := map[string]bool{}
	for path, item := range api.Paths.Map() {
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") || strings.Contains(path, "..") {
			return nil, errors.New("invalid OpenAPI operation path")
		}
		for method, op := range item.Operations() {
			name := op.OperationID
			if name == "" {
				name = strings.ToLower(method) + " " + path
			}
			if names[name] {
				return nil, errors.New("duplicate OpenAPI operation ID")
			}
			names[name] = true
			fields := map[string]any{}
			required := []string{}
			groups := map[string]map[string]any{}
			groupRequired := map[string][]string{}
			params := append(append(openapi3.Parameters{}, item.Parameters...), op.Parameters...)
			for _, pr := range params {
				p := pr.Value
				if p == nil || p.Schema == nil || p.Schema.Value == nil || (p.In != "path" && p.In != "query") {
					return nil, errors.New("OpenAPI adapter supports path and query parameters with schemas")
				}
				if p.Style != "" && p.Style != "simple" && p.Style != "form" {
					return nil, errors.New("unsupported OpenAPI parameter serialization")
				}
				if p.Schema.Value.Type == nil || (!p.Schema.Value.Type.Is("string") && !p.Schema.Value.Type.Is("integer") && !p.Schema.Value.Type.Is("number") && !p.Schema.Value.Type.Is("boolean")) {
					return nil, errors.New("OpenAPI path and query parameters must be scalar")
				}
				if groups[p.In] == nil {
					groups[p.In] = map[string]any{}
				}
				groups[p.In][p.Name] = p.Schema.Value
				if p.Required {
					groupRequired[p.In] = append(groupRequired[p.In], p.Name)
				}
			}
			for group, props := range groups {
				fields[group] = object(props, groupRequired[group]...)
				if len(groupRequired[group]) > 0 {
					required = append(required, group)
				}
			}
			if op.RequestBody != nil {
				b := op.RequestBody.Value
				if b == nil || b.Content["application/json"] == nil || b.Content["application/json"].Schema == nil {
					return nil, errors.New("OpenAPI request bodies must use application/json")
				}
				fields["body"] = b.Content["application/json"].Schema.Value
				if b.Required {
					required = append(required, "body")
				}
			}
			sort.Strings(required)
			schema, _ := json.Marshal(object(fields, required...))
			if len(schema) > 32<<10 {
				return nil, errors.New("OpenAPI tool schema exceeds 32 KiB")
			}
			desc := op.Summary
			if desc == "" {
				desc = op.Description
			}
			if len(desc) > 1000 {
				desc = desc[:1000]
			}
			tools = append(tools, Tool{Name: name, Description: desc, InputSchema: schema, Method: method, Path: path})
			if len(tools) > MaxTools {
				return nil, errors.New("OpenAPI document exceeds 200 operations")
			}
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}
func expandRefs(value, root any, depth int) (any, error) {
	budget := 20000
	return expandRefBudget(value, root, depth, &budget)
}
func expandRefBudget(value, root any, depth int, budget *int) (any, error) {
	*budget--
	if *budget < 0 {
		return nil, errors.New("schema expansion exceeds limits")
	}
	if depth > 30 {
		return nil, errors.New("schema references are recursive or too deep")
	}
	switch v := value.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			if !strings.HasPrefix(ref, "#/") {
				return nil, errors.New("only local OpenAPI references are supported")
			}
			target := root
			for _, key := range strings.Split(ref[2:], "/") {
				key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
				o, ok := target.(map[string]any)
				if !ok {
					return nil, errors.New("invalid schema reference")
				}
				target, ok = o[key]
				if !ok {
					return nil, errors.New("unknown schema reference")
				}
			}
			return expandRefBudget(target, root, depth+1, budget)
		}
		out := map[string]any{}
		for k, x := range v {
			y, err := expandRefBudget(x, root, depth+1, budget)
			if err != nil {
				return nil, err
			}
			out[k] = y
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			y, err := expandRefBudget(x, root, depth+1, budget)
			if err != nil {
				return nil, err
			}
			out[i] = y
		}
		return out, nil
	default:
		return value, nil
	}
}

func (m *Manager) invokeHTTP(ctx context.Context, r *record, secret string, tool Tool, args map[string]any) ([]byte, error) {
	tools := builtinTools(r.Account.Integration)
	if r.Account.Integration == "openapi" {
		var err error
		tools, err = openAPITools(r.Spec)
		if err != nil {
			return nil, err
		}
	}
	var t *Tool
	for _, candidate := range tools {
		if candidate.Name == tool.Name {
			v := candidate
			t = &v
		}
	}
	if t == nil || fingerprint(*t) != fingerprint(tool) {
		return nil, errors.New("tool definition changed; refresh and review authorization")
	}
	return m.dispatchHTTP(ctx, r, r.Account.Endpoint, secret, *t, args)
}

func (m *Manager) dispatchHTTP(ctx context.Context, r *record, endpoint, secret string, t Tool, args map[string]any) ([]byte, error) {
	path := t.Path
	if params, ok := args["path"].(map[string]any); ok {
		for k, v := range params {
			s := fmt.Sprint(v)
			if strings.ContainsAny(s, "/\\%?#") || s == "." || s == ".." {
				return nil, errors.New("path arguments must be single path segments")
			}
			path = strings.ReplaceAll(path, "{"+k+"}", url.PathEscape(s))
		}
	}
	if strings.ContainsAny(path, "{}") {
		return nil, errors.New("missing path argument")
	}
	query := url.Values{}
	if params, ok := args["query"].(map[string]any); ok {
		for k, v := range params {
			query.Set(k, fmt.Sprint(v))
		}
	}
	if r.Account.Integration == "github" && strings.HasPrefix(t.Name, "list_") && query.Get("per_page") == "" {
		query.Set("per_page", "30")
	}
	if r.Account.Integration == "notion" && t.Name == "list_block_children" && query.Get("page_size") == "" {
		query.Set("page_size", "30")
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var body []byte
	if b, ok := args["body"]; ok {
		body, _ = json.Marshal(b)
	}
	return m.request(ctx, r, endpoint, secret, t.Method, path, bytes.NewReader(body))
}
