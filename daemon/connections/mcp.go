package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A remote MCP call waits at least this long, and longer when its arguments
// ask the service to wait (Onlora's wait_s), up to the media ceiling.
const (
	mcpCallTimeout = 2 * time.Minute
	mcpWaitMargin  = 30 * time.Second
	mcpMaxTimeout  = MediaCallTimeout
	// A service's own error text is passed on, cut to this many bytes.
	maxServiceErrorBytes = 2000
)

// isTransportError matches one of bearerTransport's fixed errors. The SDK
// keeps some transport errors only as text, so the message counts too.
func isTransportError(err, target error) bool {
	return err != nil && (errors.Is(err, target) || strings.Contains(err.Error(), target.Error()))
}

func usesMCP(r *record) bool {
	return r.Account.Integration == "mcp" || r.Account.Integration == "linear" || r.Account.AuthMethod == "mcp_oauth"
}

// serviceError is an answer the service itself gave: a tool result marked
// isError or a JSON-RPC error. It says nothing about the account's health.
type serviceError struct{ service, detail string }

func (e serviceError) Error() string {
	if e.detail == "" {
		return e.service + " reported an error without details"
	}
	return e.service + " reported an error: " + e.detail
}

// toolUpdatedError is a call to a tool whose definition changed upstream
// since the person allowed it. The call was not sent.
type toolUpdatedError struct{ service, tool string }

func (e toolUpdatedError) Error() string {
	return fmt.Sprintf("%s updated the tool %q; it runs again once the person confirms the update in Mewla Connections", e.service, e.tool)
}

// timeoutError is a call that ran out of time after it was sent. The service
// may have done the work, so Mewla never retries it.
type timeoutError struct {
	service string
	after   time.Duration
}

func (e timeoutError) Error() string {
	return fmt.Sprintf("%s did not answer within %s; the request may already have run there. Mewla did not retry it. Check its state with a read tool (for example a job or project status) before calling it again", e.service, e.after.Round(time.Second))
}

func accountService(r *record) string {
	if name := serviceName(r.Account.Integration); name != "" && r.Account.Integration != "mcp" {
		return name
	}
	if r.Account.Name != "" {
		return r.Account.Name
	}
	return "The MCP service"
}

// cutText trims s to at most n bytes on a rune boundary.
func cutText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// mcpClient is one session's HTTP client. Production sessions get their own
// checked transport that keeps its connection open for the session's few
// requests, and leaves waiting to the call's deadline.
func (m *Manager) mcpClient(r *record, secret string) (*http.Client, func()) {
	u, _ := url.Parse(r.Account.Endpoint)
	var client http.Client
	closeIdle := func() {}
	if m.networkOwned {
		port := u.Port()
		if port == "" {
			port = "443"
			if u.Scheme == "http" {
				port = "80"
			}
		}
		transport := checkedTransport(u.Hostname()+":"+port, r.Account.TrustedNetworks)
		transport.DisableKeepAlives = false
		transport.ResponseHeaderTimeout = 0
		transport.MaxIdleConnsPerHost = 2
		transport.IdleConnTimeout = 30 * time.Second
		client = http.Client{Transport: transport, CheckRedirect: noRedirects}
		closeIdle = transport.CloseIdleConnections
	} else {
		client = *m.http
		client.Timeout = 0
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = bearerTransport{base: base, secret: secret, origin: u.Scheme + "://" + u.Host}
	return &client, closeIdle
}

// mcpSession opens one session. onToolsChanged runs when the service sends
// notifications/tools/list_changed on any of the session's streams.
func (m *Manager) mcpSession(ctx context.Context, r *record, secret string, onToolsChanged func()) (*mcp.ClientSession, error) {
	client, closeIdle := m.mcpClient(r, secret)
	options := &mcp.ClientOptions{}
	if onToolsChanged != nil {
		options.ToolListChangedHandler = func(context.Context, *mcp.ToolListChangedRequest) { onToolsChanged() }
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "Mewla", Version: "1"}, options)
	session, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: r.Account.Endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		closeIdle()
		// SDK errors carry status and JSON-RPC text, never the bearer header.
		log.Printf("plugins: %s MCP connect failed: %.300s", r.Account.Integration, err)
		if isTransportError(err, errAuth) {
			return nil, errAuth
		}
		if ctx.Err() != nil {
			return nil, safeError(ctx.Err())
		}
		if isTransportError(err, errNetwork) {
			return nil, fmt.Errorf("could not reach %s: %w", accountService(r), errNetwork)
		}
		return nil, fmt.Errorf("%s did not accept the MCP connection; check that its endpoint still serves MCP", accountService(r))
	}
	go func() {
		_ = session.Wait()
		closeIdle()
	}()
	return session, nil
}

func listMCP(ctx context.Context, s *mcp.ClientSession) ([]Tool, error) {
	tools := []Tool{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 10; page++ {
		result, err := s.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			log.Printf("plugins: MCP tools/list failed on page %d: %.300s", page+1, err)
			if isTransportError(err, errAuth) {
				return nil, errAuth
			}
			if strings.Contains(err.Error(), "exceeds size limit") {
				return nil, errors.New("MCP tool list exceeds 1 MB")
			}
			if isTransportError(err, errNetwork) {
				return nil, fmt.Errorf("MCP discovery failed: %w", errNetwork)
			}
			return nil, errors.New("MCP discovery failed")
		}
		for _, t := range result.Tools {
			if t.Name == "" || seen[t.Name] || len(t.Name) > 200 {
				return nil, errors.New("MCP returned invalid or duplicate tool names")
			}
			seen[t.Name] = true
			schema, err := json.Marshal(t.InputSchema)
			if err != nil || len(schema) > MaxToolSchemaBytes {
				return nil, fmt.Errorf("MCP tool %q schema is %d KB, over the %d KB limit", t.Name, len(schema)>>10, MaxToolSchemaBytes>>10)
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

// openMCP opens a session and lists its tools. Both steps are safe to repeat,
// so a network failure gets one more attempt; nothing has been called yet.
func (m *Manager) openMCP(ctx context.Context, r *record, secret string, onToolsChanged func()) (*mcp.ClientSession, []Tool, error) {
	for attempt := 0; ; attempt++ {
		s, err := m.mcpSession(ctx, r, secret, onToolsChanged)
		var tools []Tool
		if err == nil {
			tools, err = listMCP(ctx, s)
			if err == nil {
				return s, tools, nil
			}
			s.Close()
		}
		if attempt == 1 || ctx.Err() != nil || !isTransportError(err, errNetwork) {
			if ctx.Err() != nil {
				err = safeError(ctx.Err())
			}
			return nil, nil, err
		}
		log.Printf("plugins: %s MCP setup failed once; trying again", r.Account.Integration)
	}
}

func (m *Manager) discoverMCP(ctx context.Context, r *record, secret string) ([]Tool, error) {
	s, tools, err := m.openMCP(ctx, r, secret, nil)
	if err != nil {
		return nil, err
	}
	s.Close()
	return tools, nil
}

// mcpOutcome is one remote call. Tools is the service's current tool list
// when Mewla got one, so the caller can adopt it under the manager lock.
type mcpOutcome struct {
	Result []byte
	Tools  []Tool
	Err    error
}

// callMCP runs one tool call on a snapshot of the account. It never touches
// manager state. The tool is called only if the service still describes it
// exactly as the person allowed it.
func (m *Manager) callMCP(ctx context.Context, r *record, secret string, t Tool, args map[string]any, timeout time.Duration) mcpOutcome {
	var changed atomic.Bool
	s, current, err := m.openMCP(ctx, r, secret, func() { changed.Store(true) })
	if err != nil {
		return mcpOutcome{Err: err}
	}
	out := mcpOutcome{Tools: current}
	found := false
	for _, tool := range current {
		if tool.Name == t.Name && fingerprint(tool) == fingerprint(t) {
			found = true
		}
	}
	if !found {
		s.Close()
		out.Err = toolUpdatedError{accountService(r), t.Name}
		return out
	}
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: t.Name, Arguments: args})
	// Closing waits for notifications that arrived with the answer.
	s.Close()
	if changed.Load() && ctx.Err() == nil {
		// The service announced new tools while this call ran.
		if again, listErr := m.discoverMCP(ctx, r, secret); listErr == nil {
			out.Tools = again
		}
	}
	switch {
	case ctx.Err() != nil:
		out.Err = timeoutError{accountService(r), timeout}
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out.Err = safeError(ctx.Err())
		}
	case err != nil:
		out.Err = mcpCallError(r, err)
	case result.IsError:
		out.Err = serviceError{accountService(r), resultText(result)}
	default:
		out.Result, out.Err = json.Marshal(result)
	}
	return out
}

// mcpCallError keeps a JSON-RPC error's code and message, which the service
// wrote for the caller. Transport errors keep only a fixed message.
func mcpCallError(r *record, err error) error {
	if isTransportError(err, errAuth) {
		return errAuth
	}
	var wire *jsonrpc.Error
	// -32005 is the SDK's "rejected by transport": the HTTP request failed.
	if errors.As(err, &wire) && wire.Code != -32005 {
		return serviceError{accountService(r), cutText(fmt.Sprintf("%s (JSON-RPC %d)", wire.Message, wire.Code), maxServiceErrorBytes)}
	}
	log.Printf("plugins: %s MCP call failed: %.300s", r.Account.Integration, err)
	if isTransportError(err, errNetwork) || wire != nil {
		return fmt.Errorf("the connection to %s broke during the call; the request may already have run there. Mewla did not retry it. Check its state before calling it again", accountService(r))
	}
	return fmt.Errorf("%s returned an answer Mewla could not read; the request may already have run there. Check its state before calling it again", accountService(r))
}

// resultText is the text a service put in an error result.
func resultText(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, text.Text)
		}
	}
	if len(parts) == 0 && result.StructuredContent != nil {
		if raw, err := json.Marshal(result.StructuredContent); err == nil {
			parts = append(parts, string(raw))
		}
	}
	return cutText(strings.Join(parts, "\n"), maxServiceErrorBytes)
}

// waitArguments name an argument that asks the service to hold the request
// open, in seconds unless the name ends in _ms.
var waitArguments = []string{"wait_s", "wait_seconds", "wait", "timeout_s", "timeout_seconds", "timeout", "wait_ms", "timeout_ms"}

// mcpTimeout is how long one call may take: never less than what the call
// asks the service to wait (or the schema's default wait), plus a margin.
func mcpTimeout(t Tool, args map[string]any) time.Duration {
	var schema struct {
		Properties map[string]struct {
			Default any `json:"default"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(t.InputSchema, &schema)
	timeout := mcpCallTimeout
	for _, name := range waitArguments {
		value, ok := args[name].(float64)
		if !ok {
			value, ok = schema.Properties[name].Default.(float64)
		}
		if !ok || value <= 0 {
			continue
		}
		unit := time.Second
		if strings.HasSuffix(name, "_ms") {
			unit = time.Millisecond
		}
		if value > float64(mcpMaxTimeout/unit) {
			return mcpMaxTimeout
		}
		if wait := time.Duration(value*float64(unit)) + mcpWaitMargin; wait > timeout {
			timeout = wait
		}
	}
	return min(timeout, mcpMaxTimeout)
}

// invokeMCPAccount runs one MCP tool call. The manager lock is held only to
// check the call and to record its outcome, so calls run side by side and a
// slow one never holds up the others. handled is false for other accounts.
func (m *Manager) invokeMCPAccount(parent context.Context, q Request) (Response, bool, error) {
	m.mu.Lock()
	r, ok := m.records[q.ID]
	if !ok || m.isPending(q.ID) || !usesMCP(r) {
		m.mu.Unlock()
		return Response{}, false, nil
	}
	t, args, err := checkInvoke(r, q)
	secret := ""
	if err == nil {
		prep, cancel := context.WithTimeout(parent, 20*time.Second)
		secret, err = m.accountToken(prep, r)
		cancel()
		if err == nil && secret == "" && r.Account.Integration == "linear" {
			err = errAuth
		}
	}
	if err != nil {
		if isTransportError(err, errAuth) {
			r.Account.Status = "authorization_required"
			m.event(r, q.Tool, err)
			_ = m.save()
		}
		m.mu.Unlock()
		return Response{}, true, err
	}
	snapshot := *r
	snapshot.Account = cloneAccount(r.Account)
	sensitive := m.sensitiveValues(r, secret)
	m.mu.Unlock()

	timeout := mcpTimeout(t, args)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	out := m.callMCP(ctx, &snapshot, secret, t, args, timeout)

	m.mu.Lock()
	defer m.mu.Unlock()
	result, err := m.finishMCP(q.ID, q.Tool, sensitive, out, true)
	if err != nil {
		return Response{}, true, err
	}
	return Response{Result: result}, true, nil
}

// finishMCP records a finished call on its account, looked up again: it may
// have been changed or disconnected while the call ran. A tool list the
// service sent is adopted. Only a failure to reach or sign in to the service
// marks the account; the service's own answers and tool updates do not.
func (m *Manager) finishMCP(id, tool string, sensitive []string, out mcpOutcome, record bool) ([]byte, error) {
	result, err := out.Result, out.Err
	if err == nil && !json.Valid(result) {
		err = errors.New("integration returned invalid JSON")
	}
	if err == nil && len(result) > MaxResultBytes {
		err = errors.New("integration result exceeds 1 MiB")
	}
	var service serviceError
	if errors.As(err, &service) {
		service.detail = string(redactValues([]byte(service.detail), sensitive))
		err = service
	}
	if err == nil {
		result = redactValues(result, sensitive)
	}
	r := m.records[id]
	if r == nil || r.Account.Status == "disconnected" {
		return result, err
	}
	if out.Tools != nil {
		if updated := syncTools(r, out.Tools); len(updated) > 0 {
			log.Printf("plugins: %s updated %d tools; waiting for confirmation: %d", r.Account.Integration, len(updated), len(r.Updates))
		}
	}
	if !record {
		if out.Tools != nil {
			_ = m.save()
		}
		return result, err
	}
	var updated toolUpdatedError
	var timedOut timeoutError
	switch {
	case isTransportError(err, errAuth):
		r.Account.Status = "authorization_required"
	case errors.As(err, &service), errors.As(err, &updated), errors.As(err, &timedOut):
		// The service answered, or is still working: not an account fault.
	case err != nil:
		r.Account.Status = "error"
	default:
		r.Account.Status = "connected"
		now := time.Now().UTC()
		r.Account.VerifiedAt = &now
	}
	history := err
	if errors.As(err, &service) {
		history = errors.New(cutText(service.Error(), 300))
	}
	m.event(r, tool, history)
	if saveErr := m.save(); saveErr != nil {
		return nil, errors.New("call finished but history could not be saved; inspect before retrying")
	}
	return result, err
}

// Identity is optional in MCP. Only call a reviewed identity operation on a
// fixed official resource, using its discovered structured input contract.
func (m *Manager) officialMCPIdentity(ctx context.Context, r *record, secret string, tools []Tool) string {
	for _, tool := range tools {
		args := map[string]any{}
		if r.Account.Integration == "notion" && tool.Name == "notion-get-self" && toolGroup(r, tool) == "read" {
		} else if r.Account.Integration == "linear" && tool.Name == "get_user" && toolGroup(r, tool) == "read" {
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if json.Unmarshal(tool.InputSchema, &schema) != nil || schema.Properties["query"] == nil {
				continue
			}
			args["query"] = "me"
		} else {
			continue
		}
		out := m.callMCP(ctx, r, secret, tool, args, 0)
		if out.Err != nil {
			return ""
		}
		var result struct {
			Structured map[string]any `json:"structuredContent"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(out.Result, &result) != nil {
			return ""
		}
		identity := func(value map[string]any) string {
			if nested, ok := value["user"].(map[string]any); ok {
				value = nested
			}
			for _, key := range []string{"email", "name", "id"} {
				if name, ok := value[key].(string); ok && len(name) > 0 && len(name) <= 200 {
					return name
				}
			}
			return ""
		}
		if value := identity(result.Structured); value != "" {
			return value
		}
		for _, content := range result.Content {
			if content.Type == "text" {
				var value map[string]any
				if json.Unmarshal([]byte(content.Text), &value) == nil {
					if name := identity(value); name != "" {
						return name
					}
				}
			}
		}
	}
	return ""
}
