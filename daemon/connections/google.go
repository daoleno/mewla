package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/daoleno/mewla/daemon/googleauth"
)

// Google Calendar and Gmail are two plugins with one sign-in path. Each asks
// Google only for its own scopes (see googleauth.Scopes), so connecting
// Calendar never grants mail access and the reverse.
const (
	googleCalendarID = "google-calendar"
	gmailID          = "gmail"

	googleScopeBase = "https://www.googleapis.com/auth/"
	userinfoHost    = "https://openidconnect.googleapis.com"
)

// googleService names the exchange service and API host behind a plugin.
func googleService(kind string) (service, endpoint string, ok bool) {
	switch kind {
	case googleCalendarID:
		return googleauth.Calendar, "https://www.googleapis.com", true
	case gmailID:
		return googleauth.Gmail, "https://gmail.googleapis.com", true
	}
	return "", "", false
}

func isGoogle(kind string) bool {
	_, _, ok := googleService(kind)
	return ok
}

func googleScopes(kind string, writes bool) []string {
	service, _, _ := googleService(kind)
	return googleauth.Scopes(service, writes)
}

// googleTool is one reviewed operation. scopes lists the short names of the
// Google scopes that each allow it; the account's granted scopes decide
// whether the tool is offered at all.
type googleTool struct {
	tool   Tool
	scopes []string
	run    func(*googleCall, map[string]any) (any, error)
}

func googleTools(kind string) []googleTool {
	tools := []googleTool{{
		tool:   googleToolDef("get_me", "Read the signed-in Google account (email and name)", true, object(map[string]any{})),
		scopes: []string{"openid"},
		run: func(c *googleCall, _ map[string]any) (any, error) {
			var me struct {
				Email string `json:"email"`
				Name  string `json:"name"`
			}
			if err := c.do(userinfoHost, "GET", "/v1/userinfo", nil, nil, &me); err != nil {
				return nil, err
			}
			return map[string]any{"email": me.Email, "name": me.Name}, nil
		},
	}}
	switch kind {
	case googleCalendarID:
		return append(tools, calendarTools()...)
	case gmailID:
		return append(tools, gmailTools()...)
	}
	return nil
}

func googleToolDef(name, description string, read bool, schema map[string]any) Tool {
	raw, _ := json.Marshal(schema)
	return Tool{Name: name, Description: description, InputSchema: raw, Read: read}
}

func googleReviewedTools(kind string) []Tool {
	var tools []Tool
	for _, op := range googleTools(kind) {
		tools = append(tools, op.tool)
	}
	return tools
}

func googleAllowed(op googleTool, scopes []string) bool {
	for _, scope := range op.scopes {
		if scope != "openid" {
			scope = googleScopeBase + scope
		}
		if contains(scopes, scope) {
			return true
		}
	}
	return false
}

func (m *Manager) discoverGoogle(ctx context.Context, r *record, secret string) error {
	raw, err := m.request(ctx, r, userinfoHost, secret, "GET", "/v1/userinfo", nil)
	if err != nil {
		return err
	}
	var identity struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.Sub == "" {
		return errors.New("Google did not return account identity")
	}
	r.Account.Identity = identity.Email
	if r.Account.Identity == "" {
		r.Account.Identity = identity.Sub
	}
	r.Account.Tools = nil
	for _, op := range googleTools(r.Account.Integration) {
		if googleAllowed(op, r.Account.Scopes) {
			r.Account.Tools = append(r.Account.Tools, op.tool)
		}
	}
	return nil
}

func (m *Manager) invokeGoogle(ctx context.Context, r *record, secret string, t Tool, args map[string]any) ([]byte, error) {
	for _, op := range googleTools(r.Account.Integration) {
		if op.tool.Name == t.Name && fingerprint(op.tool) == fingerprint(t) && googleAllowed(op, r.Account.Scopes) {
			result, err := op.run(&googleCall{ctx: ctx, m: m, r: r, secret: secret, endpoint: r.Account.Endpoint}, args)
			if err != nil {
				return nil, err
			}
			return json.Marshal(result)
		}
	}
	return nil, errors.New("Google tool unavailable for the authorized scopes; reconnect with the needed access")
}

// googleCall is one tool call's access to Google's API.
type googleCall struct {
	ctx      context.Context
	m        *Manager
	r        *record
	secret   string
	endpoint string
}

// do sends one request and decodes the JSON answer into out (when given).
func (c *googleCall) do(endpoint, method, path string, query url.Values, body, out any) error {
	if endpoint == "" {
		endpoint = c.endpoint
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	raw, err := c.m.request(c.ctx, c.r, endpoint, c.secret, method, path, reader)
	if err != nil {
		return googleError(err)
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return errors.New("Google returned an unreadable answer")
	}
	return nil
}

// googleError says what a Google refusal means for this person; the bare
// status would send Brain guessing.
func googleError(err error) error {
	var status StatusError
	if !errors.As(err, &status) {
		return err
	}
	switch status.Status {
	case 400:
		return errors.New("Google rejected the request (HTTP 400). Check the arguments, such as times, addresses and ids")
	case 403:
		return errors.New("Google refused this call (HTTP 403). The sign-in may not include this access; reconnect and allow changes if this one changes things")
	case 404:
		return errors.New("Google could not find that item (HTTP 404)")
	case 429:
		return errors.New("Google is limiting this account for now (HTTP 429). Try again later")
	}
	return err
}

// Argument readers. The input schema has already checked every type.

func argString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

func argInt(args map[string]any, key string, fallback int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return fallback
}

func argBool(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

func argStrings(args map[string]any, key string) []string {
	var out []string
	list, _ := args[key].([]any)
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// Schema pieces.

func text(max int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
}

func choice(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func intRange(min, max int) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "maximum": max}
}

func list(item map[string]any, max int) map[string]any {
	return map[string]any{"type": "array", "items": item, "maxItems": max}
}

func truncate(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut], true
}

func pathID(id string) string { return url.PathEscape(id) }

var errNoChange = fmt.Errorf("name at least one thing to change")
