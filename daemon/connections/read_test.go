package connections

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestReadToolHonorsSwitchGrantsAndLeavesHistory(t *testing.T) {
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/user":
			return reply(200, `{"id":1,"login":"octo"}`), nil
		case strings.HasSuffix(r.URL.Path, "/actions/runs"):
			if r.URL.Query().Get("branch") != "main" {
				return reply(400, `{}`), nil
			}
			response := reply(200, `{"workflow_runs":[{"id":7,"note":"secret-token"}]}`)
			response.Header.Set("X-RateLimit-Remaining", "4999")
			return response, nil
		case strings.HasSuffix(r.URL.Path, "/repos/o/missing"):
			return reply(404, `{}`), nil
		}
		return reply(200, `{}`), nil
	})
	account := addGitHub(t, m, "work", "secret-token")
	historyBefore := len(mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account.History)

	result, err := m.ReadTool(context.Background(), account.ID, "list_workflow_runs",
		map[string]any{"path": map[string]any{"owner": "o", "repo": "r"}, "query": map[string]any{"branch": "main", "per_page": 5}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Body), "secret-token") || result.RateRemaining != 4999 {
		t.Fatalf("body %s remaining %d", result.Body, result.RateRemaining)
	}

	// A refusal is a typed status and does not mark the account broken.
	_, err = m.ReadTool(context.Background(), account.ID, "get_repository", map[string]any{"path": map[string]any{"owner": "o", "repo": "missing"}})
	var status StatusError
	if !errors.As(err, &status) || status.Status != 404 {
		t.Fatalf("missing repository: %v", err)
	}
	got := mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account
	if got.Status != "connected" || len(got.History) != historyBefore {
		t.Fatalf("a background read changed the account: %s, %d events", got.Status, len(got.History))
	}

	// Writes never qualify, even when allowed.
	mustHandle(t, m, Request{Action: "policy", ID: account.ID, Tool: "create_issue", Allowed: true})
	if _, err := m.ReadTool(context.Background(), account.ID, "create_issue", map[string]any{}); !errors.Is(err, ErrReadNotAllowed) {
		t.Fatalf("write tool: %v", err)
	}
	// A read the person turned off is refused.
	mustHandle(t, m, Request{Action: "policy", ID: account.ID, Tool: "list_workflow_runs", Allowed: false})
	if _, err := m.ReadTool(context.Background(), account.ID, "list_workflow_runs", map[string]any{"path": map[string]any{"owner": "o", "repo": "r"}}); !errors.Is(err, ErrReadNotAllowed) {
		t.Fatalf("revoked read: %v", err)
	}
	accounts := m.ReadAccounts("github")
	if len(accounts) != 1 || accounts[0].Reads["list_workflow_runs"] || !accounts[0].Reads["get_repository"] {
		t.Fatalf("read accounts: %+v", accounts)
	}
	// A switched-off account reads nothing and is still listed.
	mustHandle(t, m, Request{Action: "disable", ID: account.ID})
	if _, err := m.ReadTool(context.Background(), account.ID, "get_repository", map[string]any{"path": map[string]any{"owner": "o", "repo": "r"}}); !errors.Is(err, ErrAccountOff) {
		t.Fatalf("disabled account: %v", err)
	}
	if accounts := m.ReadAccounts("github"); len(accounts) != 1 || accounts[0].Enabled {
		t.Fatalf("disabled account listing: %+v", accounts)
	}
}
