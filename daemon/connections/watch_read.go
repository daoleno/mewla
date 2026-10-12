package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Watch reads connected accounts on the daemon's own behalf to collect
// project signals. It is a separate, narrower boundary than Brain's tool
// grants: GET-only requests to a fixed set of GitHub paths and a fixed set of
// Linear read tools. These reads never change account status or history and
// never hold the manager lock across the network call.

var watchGitHubPaths = []*regexp.Regexp{
	regexp.MustCompile(`^/repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`),
	regexp.MustCompile(`^/repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/actions/runs$`),
	regexp.MustCompile(`^/repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/issues$`),
	regexp.MustCompile(`^/repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/dependabot/alerts$`),
	regexp.MustCompile(`^/repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/code-scanning/alerts$`),
}

var watchLinearTools = map[string]bool{
	"list_teams":    true,
	"list_issues":   true,
	"list_comments": true,
	"get_issue":     true,
}

// WatchResponse is one read's outcome. Status is the HTTP status (0 when the
// request never reached the service); RateRemaining is -1 when unknown.
type WatchResponse struct {
	Body          []byte
	Status        int
	RateRemaining int
	RateReset     time.Time
}

// WatchAccounts lists enabled, connected accounts of one integration.
func (m *Manager) WatchAccounts(integration string) []Account {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Account
	for _, r := range m.records {
		if m.isPending(r.Account.ID) || r.Account.Integration != integration ||
			!r.Account.Enabled || r.Account.Status == "disconnected" {
			continue
		}
		account := cloneAccount(r.Account)
		account.Tools, account.History = nil, nil
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// watchCredential snapshots one account and its current access token.
func (m *Manager) watchCredential(ctx context.Context, accountID, integration string) (record, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.records[accountID]
	if r == nil || m.isPending(accountID) || r.Account.Integration != integration {
		return record{}, "", errors.New("plugin account not found")
	}
	if !r.Account.Enabled || r.Account.Status == "disconnected" {
		return record{}, "", errors.New("plugin account is disabled or disconnected")
	}
	secret, err := m.accountToken(ctx, r)
	if err != nil {
		return record{}, "", err
	}
	if secret == "" {
		return record{}, "", errAuth
	}
	snapshot := *r
	snapshot.Account = cloneAccount(r.Account)
	return snapshot, secret, nil
}

// WatchGitHub performs one GET against an allowed GitHub repository path.
func (m *Manager) WatchGitHub(ctx context.Context, accountID, path string, query url.Values) (WatchResponse, error) {
	allowed := false
	for _, pattern := range watchGitHubPaths {
		allowed = allowed || pattern.MatchString(path)
	}
	if !allowed {
		return WatchResponse{}, fmt.Errorf("watch read path %q is not allowed", path)
	}
	r, secret, err := m.watchCredential(ctx, accountID, "github")
	if err != nil {
		return WatchResponse{}, err
	}
	endpoint, err := trustedEndpointURL(r.Account.Endpoint, r.Account.TrustedNetworks)
	if err != nil {
		return WatchResponse{}, err
	}
	target := *endpoint
	target.Path = strings.TrimRight(endpoint.Path, "/") + path
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return WatchResponse{}, errors.New("invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Mewla-Watch")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := m.clientFor(&r).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return WatchResponse{}, safeError(ctx.Err())
		}
		return WatchResponse{}, errors.New("integration network request failed")
	}
	defer resp.Body.Close()
	out := WatchResponse{Status: resp.StatusCode, RateRemaining: -1}
	if remaining, convErr := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining")); convErr == nil {
		out.RateRemaining = remaining
	}
	if reset, convErr := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); convErr == nil {
		out.RateReset = time.Unix(reset, 0).UTC()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResultBytes+1))
	if err != nil {
		return out, errors.New("integration response could not be read")
	}
	if len(body) > MaxResultBytes {
		return out, errors.New("integration response exceeds 1 MiB; request a smaller page")
	}
	out.Body = redactValues(body, m.sensitiveValues(&r, secret))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	return out, nil
}

// WatchLinear calls one allowed Linear read tool and returns its structured
// result as JSON.
func (m *Manager) WatchLinear(ctx context.Context, accountID, tool string, args map[string]any) ([]byte, error) {
	if !watchLinearTools[tool] {
		return nil, fmt.Errorf("watch read tool %q is not allowed", tool)
	}
	r, secret, err := m.watchCredential(ctx, accountID, "linear")
	if err != nil {
		return nil, err
	}
	session, err := m.mcpSession(ctx, &r, secret)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, errors.New("Linear read failed")
	}
	if result.IsError {
		return nil, errors.New("Linear reported an error for this read")
	}
	var raw []byte
	if result.StructuredContent != nil {
		raw, err = json.Marshal(result.StructuredContent)
	} else {
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				raw = []byte(text.Text)
				break
			}
		}
	}
	if err != nil || len(raw) == 0 {
		return nil, errors.New("Linear returned no readable content")
	}
	if len(raw) > MaxResultBytes {
		return nil, errors.New("Linear result exceeds 1 MiB")
	}
	return redactValues(raw, m.sensitiveValues(&r, secret)), nil
}
