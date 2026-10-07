package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/stats"
	"github.com/gorilla/websocket"
)

// A Custom Provider changes inference routing, not the native Claude usage
// source. Exercise the normal JSONL -> collector -> get_stats wire boundary.
func TestNativeClaudeUsageReachesStatsWithCustomProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "")
	dir := filepath.Join(home, ".claude", "projects", "-tmp-native-claude")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	var lines []string
	// Claude appends multiple assistant content blocks for one message. Usage
	// repeats, then final output grows; it must be counted once at its maximum.
	for _, output := range []int{4, 9, 9} {
		raw, err := json.Marshal(map[string]any{
			"type": "assistant", "timestamp": timestamp, "sessionId": "native-one", "cwd": "/tmp/native-claude",
			"message": map[string]any{"id": "message-one", "model": "claude-sonnet-4-6",
				"content": []map[string]string{{"type": "text", "text": "PRIVATE_TRANSCRIPT_CANARY"}},
				"usage":   map[string]any{"input_tokens": 11, "output_tokens": output, "cache_read_input_tokens": 100, "cache_creation_input_tokens": 20, "service_tier": "standard"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
	}
	// A second actual message is additive, even with the same timestamp/model.
	lines = append(lines, `{"type":"assistant","timestamp":"`+timestamp+`","cwd":"/tmp/native-claude","message":{"id":"message-two","model":"claude-sonnet-4-6","usage":{"input_tokens":2,"output_tokens":3}}}`)
	// Unknown reference pricing must keep the observed usage, not become $0 known.
	lines = append(lines, `{"type":"assistant","timestamp":"`+timestamp+`","cwd":"/tmp/native-claude","message":{"id":"message-three","model":"custom-unpriced-claude-model","usage":{"input_tokens":7,"output_tokens":8}}}`)
	if err := os.WriteFile(filepath.Join(dir, "native-one.jsonl"), []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	collector := stats.NewCollector()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector.Start(ctx)
	owner := startProfileOwner(t)
	profile := modelprofiles.Profile{
		ID: "custom-claude", Name: "Custom Claude", ExecutorID: modelprofiles.ExecutorClaude,
		ProviderID: "custom", ProviderLabel: "Custom Gateway", Protocol: modelprofiles.ProtocolAnthropicMessages,
		ClientModel: "claude-sonnet-4-6", Model: "claude-sonnet-4-6",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.invalid", AuthMode: modelprofiles.AuthModeXAPIKeyEnv, CredentialEnv: "FIXTURE_API_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderConnection("claude", profile.ID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	srv := New(nil, nil, nil, collector, nil, nil, nil)
	srv.SetModelProfiles(owner)
	upgrader := websocket.Upgrader{}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		srv.mu.Lock()
		srv.writes[conn] = &sync.Mutex{}
		srv.mu.Unlock()
		defer func() { srv.mu.Lock(); delete(srv.writes, conn); srv.mu.Unlock() }()
		_, request, err := conn.ReadMessage()
		if err == nil {
			srv.handleClientMessage(conn, request)
		}
	}))
	defer host.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"type": "get_stats", "request_id": "native-claude"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"PRIVATE_TRANSCRIPT_CANARY", "gateway.invalid", "FIXTURE_API_KEY", "claudeSubscription", "codexSubscription"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("stats leaked non-usage field %s", forbidden)
		}
	}
	var response struct {
		Type      string                      `json:"type"`
		RequestID string                      `json:"request_id"`
		Ranges    map[string]*stats.RangeData `json:"ranges"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "stats_data" || response.RequestID != "native-claude" {
		t.Fatal("wrong stats response")
	}
	for _, name := range []string{"day", "week", "month", "all"} {
		row := response.Ranges[name]
		if row == nil || row.TotalTokens != 160 || row.InputTokens != 20 || row.OutputTokens != 20 || row.CacheRead != 100 || row.CacheCreate != 20 || row.Sessions != 1 {
			t.Fatalf("%s lost/doubled native usage: %+v", name, row)
		}
		if row.CostKnown {
			t.Fatalf("%s treated partially unpriced usage as a known bill", name)
		}
		if len(row.Models) != 2 {
			t.Fatalf("%s lost model identity", name)
		}
		for _, model := range row.Models {
			switch model.ID {
			case "claude-sonnet-4-6":
				if model.TotalTokens != 145 || model.Sessions != 1 || model.CostProvenance != stats.CostProvenanceEstimated || model.CostReported != 0 {
					t.Fatalf("wrong Claude estimate: %+v", model)
				}
			case "custom-unpriced-claude-model":
				if model.TotalTokens != 15 || model.CostKnown || model.CostProvenance != stats.CostProvenanceUnknown {
					t.Fatalf("unknown model fabricated pricing: %+v", model)
				}
			default:
				t.Fatalf("unexpected model %s", model.ID)
			}
		}
	}
}
