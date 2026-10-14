package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Background readers such as Watch read connected accounts on the daemon's
// own behalf. They go through the same Plugin boundary as Brain: the account
// must be switched on, the tool must be one the person allowed, and the
// arguments must match its schema. Only reviewed read tools qualify. A read
// leaves the account's status and history alone, so a routine poll never
// crowds out or misreports the person's own calls, and the network call runs
// outside the manager lock.

// ReadAccount is one account as a background reader sees it.
type ReadAccount struct {
	ID          string `json:"id"`
	Integration string `json:"integration"`
	Name        string `json:"name"`
	// Enabled is false when the account is switched off or disconnected.
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
	// Reads lists the account's reviewed read tools; true means allowed.
	Reads map[string]bool `json:"reads,omitempty"`
}

// ReadResult is one read's outcome. RateRemaining is -1 when the service did
// not report it.
type ReadResult struct {
	Body          []byte
	RateRemaining int
	RateReset     time.Time
}

// ErrReadNotAllowed reports a read tool the person has not allowed.
var ErrReadNotAllowed = errors.New("read tool is not allowed for this account")

// ErrAccountOff reports an account that is switched off or disconnected.
var ErrAccountOff = errors.New("plugin account is disabled or disconnected")

// ReadAccounts lists every account of one integration, including ones that
// are switched off, so a reader can say why it is not reading.
func (m *Manager) ReadAccounts(integration string) []ReadAccount {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ReadAccount
	for _, r := range m.records {
		if m.isPending(r.Account.ID) || r.Account.Integration != integration {
			continue
		}
		account := ReadAccount{
			ID: r.Account.ID, Integration: integration, Name: r.Account.Name, Status: r.Account.Status,
			Enabled: r.Account.Enabled && r.Account.Status != "disconnected", Reads: map[string]bool{},
		}
		for _, tool := range r.Account.Tools {
			if toolGroup(r, tool) == "read" {
				account.Reads[tool.Name] = r.Grants[tool.Name] == fingerprint(tool)
			}
		}
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ReadTool runs one allowed read tool. A service's HTTP refusal comes back as
// StatusError.
func (m *Manager) ReadTool(ctx context.Context, accountID, tool string, args map[string]any) (ReadResult, error) {
	if m == nil {
		return ReadResult{}, errors.New("plugins are unavailable")
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		return ReadResult{}, errors.New("tool arguments must be a JSON object")
	}
	m.mu.Lock()
	r := m.records[accountID]
	if r == nil || m.isPending(accountID) {
		m.mu.Unlock()
		return ReadResult{}, errors.New("plugin account not found")
	}
	if !r.Account.Enabled || r.Account.Status == "disconnected" {
		m.mu.Unlock()
		return ReadResult{}, ErrAccountOff
	}
	granted := false
	for _, candidate := range r.Account.Tools {
		if candidate.Name == tool {
			granted = toolGroup(r, candidate) == "read" && r.Grants[tool] == fingerprint(candidate)
		}
	}
	if !granted {
		m.mu.Unlock()
		return ReadResult{}, ErrReadNotAllowed
	}
	t, checked, err := checkInvoke(r, Request{Tool: tool, Arguments: arguments})
	if err != nil {
		m.mu.Unlock()
		return ReadResult{}, err
	}
	secret, err := m.accountToken(ctx, r)
	if err == nil && secret == "" {
		err = errAuth
	}
	snapshot := *r
	snapshot.Account = cloneAccount(r.Account)
	var sensitive []string
	if err == nil {
		sensitive = m.sensitiveValues(r, secret)
	}
	m.mu.Unlock()
	if err != nil {
		return ReadResult{}, err
	}

	out := ReadResult{RateRemaining: -1}
	var headers http.Header
	var body []byte
	if usesMCP(&snapshot) {
		timeout := mcpTimeout(t, checked)
		call, cancel := context.WithTimeout(ctx, timeout)
		outcome := m.callMCP(call, &snapshot, secret, t, checked, timeout)
		cancel()
		// A routine read leaves status and history alone, but a tool list it
		// brought back is adopted like any other.
		m.mu.Lock()
		body, err = m.finishMCP(accountID, tool, sensitive, outcome, false)
		m.mu.Unlock()
	} else if snapshot.Account.Integration == "google" {
		body, err = m.invokeGoogle(ctx, &snapshot, secret, t, checked)
	} else {
		body, err = m.invokeHTTP(ctx, &snapshot, secret, t, checked, &headers)
	}
	if remaining, convErr := strconv.Atoi(headers.Get("X-RateLimit-Remaining")); convErr == nil {
		out.RateRemaining = remaining
	}
	if reset, convErr := strconv.ParseInt(headers.Get("X-RateLimit-Reset"), 10, 64); convErr == nil {
		out.RateReset = time.Unix(reset, 0).UTC()
	}
	if ctx.Err() != nil {
		return out, safeError(ctx.Err())
	}
	if err != nil {
		return out, err
	}
	if !json.Valid(body) {
		return out, errors.New("integration returned invalid JSON")
	}
	if len(body) > MaxResultBytes {
		return out, errors.New("integration result exceeds 1 MiB")
	}
	out.Body = redactValues(body, sensitive)
	return out, nil
}
