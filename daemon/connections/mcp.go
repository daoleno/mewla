package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (m *Manager) mcpSession(ctx context.Context, r *record, secret string) (*mcp.ClientSession, error) {
	u, _ := url.Parse(r.Account.Endpoint)
	client := *m.clientFor(r)
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = bearerTransport{base: base, secret: secret, origin: u.Scheme + "://" + u.Host}
	c := mcp.NewClient(&mcp.Implementation{Name: "Zen", Version: "1"}, nil)
	session, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: r.Account.Endpoint, HTTPClient: &client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		if errors.Is(err, errAuth) {
			return nil, errAuth
		}
		if ctx.Err() != nil {
			return nil, safeError(ctx.Err())
		}
		return nil, errors.New("MCP connection failed; check endpoint and account authorization")
	}
	return session, nil
}
func listMCP(ctx context.Context, s *mcp.ClientSession) ([]Tool, error) {
	tools := []Tool{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 10; page++ {
		result, err := s.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, errors.New("MCP discovery failed")
		}
		for _, t := range result.Tools {
			if t.Name == "" || seen[t.Name] || len(t.Name) > 200 {
				return nil, errors.New("MCP returned invalid or duplicate tool names")
			}
			seen[t.Name] = true
			schema, err := json.Marshal(t.InputSchema)
			if err != nil || len(schema) > 32<<10 {
				return nil, errors.New("MCP tool schema exceeds limits")
			}
			desc := t.Description
			if len(desc) > 1000 {
				desc = desc[:1000]
			}
			tools = append(tools, Tool{Name: t.Name, Description: desc, InputSchema: schema})
			if len(tools) > MaxTools {
				return nil, errors.New("MCP exposes more than 200 tools; use a narrower endpoint")
			}
		}
		cursor = result.NextCursor
		if cursor == "" {
			sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
			return tools, nil
		}
	}
	return nil, errors.New("MCP discovery pagination limit exceeded")
}
func (m *Manager) discoverMCP(ctx context.Context, r *record, secret string) ([]Tool, error) {
	s, err := m.mcpSession(ctx, r, secret)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return listMCP(ctx, s)
}
func (m *Manager) invokeMCP(ctx context.Context, r *record, secret string, t Tool, args map[string]any) ([]byte, error) {
	s, err := m.mcpSession(ctx, r, secret)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	// Re-discover at the actual invocation boundary: upstream cannot silently
	// change a previously granted tool schema, including calls made in batches.
	current, err := listMCP(ctx, s)
	if err != nil {
		return nil, err
	}
	found := false
	for _, tool := range current {
		if tool.Name == t.Name && fingerprint(tool) == fingerprint(t) {
			found = true
		}
	}
	if !found {
		return nil, errors.New("MCP tool changed; refresh capabilities and review authorization")
	}
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: t.Name, Arguments: args})
	if err != nil {
		return nil, errors.New("MCP invocation failed; inspect the remote service before retrying writes")
	}
	if result.IsError {
		return nil, errors.New("MCP tool reported an error; inspect the remote service before retrying writes")
	}
	return json.Marshal(result)
}
