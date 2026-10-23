package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// These are reviewed Slack Web API capabilities. Available tools are filtered
// against auth.test's X-OAuth-Scopes response; bot tokens never advertise the
// user-only search.messages operation, even with misleading token annotations.
var slackToolScopes = map[string][]string{
	"list_channels":   {"channels:read", "groups:read", "im:read", "mpim:read"},
	"channel_history": {"channels:history", "groups:history", "im:history", "mpim:history"},
	"search_messages": {"search:read"},
	"send_message":    {"chat:write", "chat:write:bot", "chat:write:user"},
	"update_message":  {"chat:write", "chat:write:bot", "chat:write:user"},
}

func slackTools() []Tool {
	return []Tool{
		builtin("get_me", "Read the authenticated Slack account and workspace", "GET", "/auth.test", true, nil, nil, nil),
		builtin("list_channels", "List channels visible to this account (one page)", "GET", "/conversations.list", true, nil, object(map[string]any{"limit": pageSize(), "cursor": str(), "types": str()}), nil),
		builtin("channel_history", "Read messages from a channel (one page)", "GET", "/conversations.history", true, nil, object(map[string]any{"channel": str(), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 15}, "cursor": str()}, "channel"), nil),
		builtin("search_messages", "Search messages using this user's search:read grant (one page)", "GET", "/search.messages", true, nil, object(map[string]any{"query": str(), "count": pageSize(), "page": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}}, "query"), nil),
		builtin("send_message", "Send a message to a Slack conversation", "POST", "/chat.postMessage", false, nil, nil, object(map[string]any{"channel": str(), "text": str(), "thread_ts": str()}, "channel", "text")),
		builtin("update_message", "Update a message posted by this account", "POST", "/chat.update", false, nil, nil, object(map[string]any{"channel": str(), "ts": str(), "text": str()}, "channel", "ts", "text")),
	}
}

func discoverSlack(m *Manager, ctx context.Context, r *record, secret string) ([]Tool, error) {
	var headers http.Header
	raw, err := m.request(ctx, r, r.Account.Endpoint, secret, "GET", "/auth.test", nil, &headers)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Team   string `json:"team"`
		User   string `json:"user"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.UserID == "" {
		return nil, errors.New("Slack did not return an account identity")
	}
	r.Account.Identity = identity.Team + " · " + identity.User
	granted := map[string]bool{}
	for _, scope := range strings.Split(headers.Get("X-OAuth-Scopes"), ",") {
		granted[strings.TrimSpace(scope)] = true
	}
	var tools []Tool
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
	return tools, nil
}
