package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP resources are documents a service publishes next to its tools, such as
// an Onlora project's snapshot. Reading one is a
// read: it needs the account switched on and its reading allowed, which for a
// reviewed service means at least one allowed read tool, and for a custom
// service at least one allowed tool. Tool grants never cover writes here
// because resources/read cannot change anything.

type mcpResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

type mcpResourceTemplate struct {
	URITemplate string `json:"uri_template"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

type mcpResourceList struct {
	Resources []mcpResource         `json:"resources"`
	Templates []mcpResourceTemplate `json:"resource_templates"`
	// NextCursor continues the resource listing with --cursor.
	NextCursor string `json:"next_cursor,omitempty"`
	Note       string `json:"note,omitempty"`
}

type mcpResourceContent struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mime_type,omitempty"`
	Text     string `json:"text,omitempty"`
	// Blob is base64, as the service sent it.
	Blob []byte `json:"blob,omitempty"`
}

func resourcesAllowed(r *record) bool {
	grouped := false
	for _, t := range r.Account.Tools {
		group := toolGroup(r, t)
		grouped = grouped || group != ""
	}
	for _, t := range r.Account.Tools {
		if r.Grants[t.Name] == fingerprint(t) && (!grouped || toolGroup(r, t) == "read") {
			return true
		}
	}
	return false
}

func validResourceURI(raw string) bool {
	if raw == "" || len(raw) > 2048 || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme != ""
}

// mcpResources lists or reads an account's resources outside the manager
// lock, like a tool call.
func (m *Manager) mcpResources(parent context.Context, q Request) (Response, error) {
	m.mu.Lock()
	r, ok := m.records[q.ID]
	if !ok || m.isPending(q.ID) {
		m.mu.Unlock()
		return Response{}, errors.New("plugin account not found")
	}
	var err error
	switch {
	case !usesMCP(r):
		err = errors.New("only MCP accounts offer resources")
	case !r.Account.Enabled || r.Account.Status == "disconnected":
		err = ErrAccountOff
	case !resourcesAllowed(r):
		err = errors.New("reading resources needs this account's reading allowed in Extensions")
	case q.Action == "read_resource" && !validResourceURI(q.URI):
		err = errors.New("name the resource with --uri, as listed by resources (up to 2048 characters)")
	case len(q.Cursor) > 4096:
		err = errors.New("invalid cursor")
	}
	secret := ""
	if err == nil {
		prep, cancel := context.WithTimeout(parent, 20*time.Second)
		secret, err = m.accountToken(prep, r)
		cancel()
	}
	if err != nil {
		m.mu.Unlock()
		return Response{}, err
	}
	snapshot := *r
	snapshot.Account = cloneAccount(r.Account)
	sensitive := m.sensitiveValues(r, secret)
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, mcpCallTimeout)
	defer cancel()
	var out mcpOutcome
	for attempt := 0; ; attempt++ {
		out = m.resourceCall(ctx, &snapshot, secret, q)
		// Listing and reading change nothing, so a dropped connection is tried once more.
		if out.Err == nil || attempt == 1 || ctx.Err() != nil || !isTransportError(out.Err, errNetwork) {
			break
		}
	}
	if ctx.Err() != nil {
		out.Err = timeoutError{accountService(r), mcpCallTimeout}
	}
	label := "resources"
	if q.Action == "read_resource" {
		label = "resource " + cutText(q.URI, 200)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result, err := m.finishMCP(q.ID, label, sensitive, out, true)
	if err != nil {
		return Response{}, err
	}
	return Response{Result: result}, nil
}

func (m *Manager) resourceCall(ctx context.Context, r *record, secret string, q Request) mcpOutcome {
	s, err := m.mcpSession(ctx, r, secret, nil)
	if err != nil {
		return mcpOutcome{Err: err}
	}
	defer s.Close()
	offered := false
	if init := s.InitializeResult(); init != nil && init.Capabilities != nil && init.Capabilities.Resources != nil {
		offered = true
	}
	if q.Action == "read_resource" {
		if !offered {
			return mcpOutcome{Err: serviceError{accountService(r), "this service offers no resources"}}
		}
		result, err := s.ReadResource(ctx, &mcp.ReadResourceParams{URI: q.URI})
		if err != nil {
			return mcpOutcome{Err: mcpCallError(r, err)}
		}
		contents := []mcpResourceContent{}
		for _, c := range result.Contents {
			if c != nil {
				contents = append(contents, mcpResourceContent{URI: c.URI, MIMEType: c.MIMEType, Text: c.Text, Blob: c.Blob})
			}
		}
		raw, err := json.Marshal(map[string]any{"contents": contents})
		return mcpOutcome{Result: raw, Err: err}
	}
	list := mcpResourceList{Resources: []mcpResource{}, Templates: []mcpResourceTemplate{}}
	if !offered {
		list.Note = "this service offers no resources"
		raw, err := json.Marshal(list)
		return mcpOutcome{Result: raw, Err: err}
	}
	resources, err := s.ListResources(ctx, &mcp.ListResourcesParams{Cursor: q.Cursor})
	if err != nil {
		return mcpOutcome{Err: mcpCallError(r, err)}
	}
	for _, res := range resources.Resources {
		if res != nil {
			list.Resources = append(list.Resources, mcpResource{URI: res.URI, Name: res.Name, Title: res.Title, Description: cutText(res.Description, 1000), MIMEType: res.MIMEType})
		}
	}
	list.NextCursor = resources.NextCursor
	if q.Cursor == "" {
		// Templates are optional; a service without them still lists resources.
		if templates, err := s.ListResourceTemplates(ctx, nil); err == nil {
			for _, tpl := range templates.ResourceTemplates {
				if tpl != nil {
					list.Templates = append(list.Templates, mcpResourceTemplate{URITemplate: tpl.URITemplate, Name: tpl.Name, Title: tpl.Title, Description: cutText(tpl.Description, 1000), MIMEType: tpl.MIMEType})
				}
			}
		}
	}
	raw, err := json.Marshal(list)
	return mcpOutcome{Result: raw, Err: err}
}
