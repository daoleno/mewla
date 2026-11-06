package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

func gwsTools() []Tool {
	props := map[string]any{"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 256}, "authorize": map[string]any{"type": "boolean", "description": "True only with explicit user authorization for this task."}}
	return []Tool{
		hlTool("whoami", "Show the signed-in Google account.", true, map[string]any{}),
		hlTool("read", "Run gws list and get-style reads on Gmail, Calendar and Drive. Prefer mewla gws <args>. Message bodies and files are read-only.", true, props, "args"),
		hlTool("execute", "Run gws with enabled writes and task-specific user authorization: sending mail, changing events, writing to Drive. Prefer mewla gws --mewla-authorize <args>. Never retried.", false, props, "args"),
	}
}
func discoverGWS(m *Manager, ctx context.Context, r *record, _ string) ([]Tool, error) {
	if r.CLI == nil || m.cloud == nil {
		return nil, errAuth
	}
	if err := m.syncGWSProfiles(ctx, true); err != nil {
		return nil, err
	}
	if r.Account.Status != "connected" {
		return nil, errAuth
	}
	return gwsTools(), nil
}
func invokeGWS(m *Manager, ctx context.Context, r *record, _ string, t Tool, args map[string]any, _ ...*http.Header) ([]byte, error) {
	if r.CLI == nil {
		return nil, errAuth
	}
	if t.Name == "whoami" {
		return json.Marshal(map[string]any{"account": r.Account.Identity, "project": r.Account.Project})
	}
	values, _ := args["args"].([]any)
	argv := []string{}
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("CLI arguments must be strings")
		}
		argv = append(argv, s)
	}
	read, err := (gwsCLI{}).classify(argv)
	if err != nil {
		return nil, err
	}
	if t.Name == "read" && !read {
		return nil, callRefused("This command requires execute and task-specific user authorization")
	}
	response, err := m.runCloud(ctx, Request{Integration: "gws", Profile: r.CLI.Profile, CLIArgs: argv, Authorize: args["authorize"] == true})
	if err != nil {
		return nil, err
	}
	return json.Marshal(response.CLIOutput)
}
