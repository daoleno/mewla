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
		case r.URL.Path == "/v1/users/me":
			return reply(200, `{"id":"u1","name":"octo"}`), nil
		case strings.HasSuffix(r.URL.Path, "/children"):
			if r.URL.Query().Get("page_size") != "5" {
				return reply(400, `{}`), nil
			}
			return reply(200, `{"results":[{"id":"b1","note":"secret-token"}]}`), nil
		case strings.HasSuffix(r.URL.Path, "/pages/missing"):
			return reply(404, `{}`), nil
		}
		return reply(200, `{}`), nil
	})
	account := addNotion(t, m, "work", "secret-token")
	historyBefore := len(mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account.History)

	result, err := m.ReadTool(context.Background(), account.ID, "list_block_children",
		map[string]any{"path": map[string]any{"id": "p1"}, "query": map[string]any{"page_size": 5}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Body), "secret-token") {
		t.Fatalf("body %s", result.Body)
	}

	// A refusal is a typed status and does not mark the account broken.
	_, err = m.ReadTool(context.Background(), account.ID, "get_page", map[string]any{"path": map[string]any{"id": "missing"}})
	var status StatusError
	if !errors.As(err, &status) || status.Status != 404 {
		t.Fatalf("missing page: %v", err)
	}
	got := mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account
	if got.Status != "connected" || len(got.History) != historyBefore {
		t.Fatalf("a background read changed the account: %s, %d events", got.Status, len(got.History))
	}

	// Writes never qualify, even when allowed.
	mustHandle(t, m, Request{Action: "policy", ID: account.ID, Tool: "update_page", Allowed: true})
	if _, err := m.ReadTool(context.Background(), account.ID, "update_page", map[string]any{}); !errors.Is(err, ErrReadNotAllowed) {
		t.Fatalf("write tool: %v", err)
	}
	// A read the person turned off is refused.
	mustHandle(t, m, Request{Action: "policy", ID: account.ID, Tool: "list_block_children", Allowed: false})
	if _, err := m.ReadTool(context.Background(), account.ID, "list_block_children", map[string]any{"path": map[string]any{"id": "p1"}}); !errors.Is(err, ErrReadNotAllowed) {
		t.Fatalf("revoked read: %v", err)
	}
	accounts := m.ReadAccounts("notion")
	if len(accounts) != 1 || accounts[0].Reads["list_block_children"] || !accounts[0].Reads["get_page"] {
		t.Fatalf("read accounts: %+v", accounts)
	}
	// A switched-off account reads nothing and is still listed.
	mustHandle(t, m, Request{Action: "disable", ID: account.ID})
	if _, err := m.ReadTool(context.Background(), account.ID, "get_page", map[string]any{"path": map[string]any{"id": "p1"}}); !errors.Is(err, ErrAccountOff) {
		t.Fatalf("disabled account: %v", err)
	}
	if accounts := m.ReadAccounts("notion"); len(accounts) != 1 || accounts[0].Enabled {
		t.Fatalf("disabled account listing: %+v", accounts)
	}
}
