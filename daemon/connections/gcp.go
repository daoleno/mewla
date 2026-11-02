package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

func gcpTools() []Tool {
	props := map[string]any{"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 2, "maxItems": 256}, "authorize": map[string]any{"type": "boolean", "description": "True only with explicit user authorization for this task."}}
	return []Tool{
		hlTool("whoami", "Show the local gcloud account, configuration and default project.", true, map[string]any{}),
		hlTool("read", "Run a reviewed gcloud read. Prefer mewla gcloud <args>. Secrets, IAM, billing and SQL require execute.", true, props, "args"),
		hlTool("execute", "Run gcloud with enabled writes and task-specific user authorization. Prefer mewla gcloud --mewla-authorize <args>. Destructive, IAM, billing, SQL and secret access need authorization. Never retried.", false, props, "args"),
	}
}
func discoverGCP(m *Manager, ctx context.Context, r *record, _ string) ([]Tool, error) {
	if r.CLI == nil || m.cloud == nil {
		return nil, errAuth
	}
	if err := m.syncGCPProfiles(ctx, true); err != nil {
		return nil, err
	}
	if r.Account.Status != "connected" {
		return nil, errAuth
	}
	return gcpTools(), nil
}
func invokeGCP(m *Manager, ctx context.Context, r *record, _ string, t Tool, args map[string]any, _ ...*http.Header) ([]byte, error) {
	if r.CLI == nil {
		return nil, errAuth
	}
	if t.Name == "whoami" {
		return json.Marshal(map[string]any{"account": r.Account.Identity, "configuration": r.Account.Profile, "project": r.Account.Project, "active": r.Account.Active})
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
	read, err := (gcloudCLI{}).classify(argv)
	if err != nil {
		return nil, err
	}
	if t.Name == "read" && !read {
		return nil, callRefused("This command requires execute and task-specific user authorization")
	}
	response, err := m.runCloud(ctx, Request{Integration: "gcp", Profile: r.CLI.Profile, CLIArgs: argv, Authorize: args["authorize"] == true})
	if err != nil {
		return nil, err
	}
	return json.Marshal(response.CLIOutput)
}
