package connections

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/daoleno/mewla/daemon/shellquote"
)

const gcpMCPEndpoint = "https://cloudcli.googleapis.com/mcp"
const gcpScope = "https://www.googleapis.com/auth/cloud-platform"
const gcpProjectsEndpoint = "https://cloudresourcemanager.googleapis.com"

// Google Cloud's broad remote tools execute commands. Only exact reviewed
// command paths and flags qualify as reads; unknown commands and SQL are writes.
// No shell or gcloud installation runs on the daemon's machine.
func gcpTools() []Tool {
	props := func() map[string]any {
		return map[string]any{
			"execution_project": map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9-]{4,61}[a-z0-9]$", "description": "Project ID from list_projects used for MCP API enablement, billing and quota. This does not select the resource project."},
			"command":           map[string]any{"type": "array", "minItems": 2, "maxItems": 100, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 16000}, "description": "Literal command words including gcloud or bq. Long flags use --name=value. Always name the resource project with --project=ID (gcloud) or --project_id=ID (bq); execution_project only pays for MCP. Example: [gcloud,compute,instances,list,--project=my-project]."},
		}
	}
	return []Tool{
		hlTool("list_projects", "Discover accessible Google Cloud projects without needing Cloud CLI Execution enabled. Returns a page; pass nextPageToken as page_token to continue. Choose the execution project from these projects based on the user's task; ask if ambiguous.", true, map[string]any{"page_token": map[string]any{"type": "string", "maxLength": 4096}}),
		hlTool("read", "Read Google Cloud resources through Google's remote Cloud CLI MCP. Accepts reviewed gcloud list, describe and get-iam-policy command paths with safe flags. Refuses credentials, arbitrary commands and SQL; use execute with task authorization for those. No local gcloud needed.", true, props(), "execution_project", "command"),
		hlTool("execute", "Run a single gcloud or bq command through Google's remote MCP with the user's IAM permissions. Changes, SQL, destructive actions, IAM and billing changes require the user's explicit authorization for this task; explain the intended effect first. Can cost money. Never retried. Google's unsupported commands remain unavailable.", false, props(), "execution_project", "command"),
	}
}

func discoverGCP(m *Manager, ctx context.Context, r *record, secret string) ([]Tool, error) {
	raw, err := m.request(ctx, r, "https://www.googleapis.com", secret, "GET", "/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	var user struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if json.Unmarshal(raw, &user) != nil || user.Email == "" || user.Sub == "" {
		return nil, errors.New("Google Cloud did not identify the signed-in account")
	}
	r.Account.Identity = user.Email
	if r.Account.Name == "Google Cloud" {
		r.Account.Name = user.Email
	}
	// Project discovery does not require the Cloud CLI API. Preserve sign-in
	// even when the account has no projects or its organization denies discovery.
	r.Account.Setup = nil
	projects, projectErr := m.gcpProjects(ctx, r, secret, "")
	var page struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
			State     string `json:"state"`
		} `json:"projects"`
		Next string `json:"nextPageToken"`
	}
	if projectErr != nil {
		r.Account.Setup = &AccountSetup{Message: "Project discovery needs access. Ask your administrator for permission to view your Google Cloud projects.", URL: "https://console.cloud.google.com/iam-admin/iam"}
	} else if json.Unmarshal(projects, &page) == nil {
		active := []string{}
		for _, p := range page.Projects {
			if p.State == "ACTIVE" {
				active = append(active, p.ProjectID)
			}
		}
		if len(active) == 0 && page.Next == "" {
			r.Account.Setup = &AccountSetup{Message: "No projects found. Create a project or ask your administrator for project access.", URL: "https://console.cloud.google.com/projectcreate"}
		} else if len(active) == 1 && page.Next == "" && gcpProjectID.MatchString(active[0]) {
			project := active[0]
			raw, err := m.request(ctx, r, "https://serviceusage.googleapis.com", secret, "GET", "/v1/projects/"+project+"/services/cloudcli.googleapis.com", nil)
			var service struct {
				State string `json:"state"`
			}
			if err != nil || json.Unmarshal(raw, &service) != nil || service.State != "ENABLED" {
				r.Account.Setup = gcpSetup(project)
			}
		}
	}
	return gcpTools(), nil
}

func (m *Manager) gcpProjects(ctx context.Context, r *record, secret, token string) ([]byte, error) {
	query := url.Values{"pageSize": {"100"}}
	if token != "" {
		query.Set("pageToken", token)
	}
	return m.request(ctx, r, gcpProjectsEndpoint, secret, "GET", "/v3/projects:search?"+query.Encode(), nil)
}

var gcpProjectID = regexp.MustCompile(`^[a-z][a-z0-9-]{4,61}[a-z0-9]$`)

// Read paths are complete command paths, never a verb guessed from arbitrary
// user words (a resource named "list" must not turn a delete into a read).
var gcpReadPaths = func() map[string]bool {
	out := map[string]bool{}
	for _, group := range []string{
		"projects", "compute instances", "compute disks", "compute networks", "compute subnetworks", "compute firewall-rules", "compute forwarding-rules", "compute addresses", "compute regions", "compute zones", "compute machine-types", "compute snapshots", "compute images",
		"container clusters", "run services", "run jobs", "run revisions", "sql instances", "sql databases", "sql backups", "storage buckets", "storage objects", "services", "iam roles", "iam service-accounts", "secrets", "secrets versions", "logging sinks", "logging metrics", "pubsub topics", "pubsub subscriptions", "functions", "artifacts repositories", "scheduler jobs", "spanner instances", "spanner databases",
	} {
		for _, verb := range []string{"list", "describe"} {
			out["gcloud "+group+" "+verb] = true
		}
	}
	out["gcloud projects get-iam-policy"] = true
	return out
}()

var gcpReadFlags = map[string]bool{"project": true, "billing-project": true, "region": true, "zone": true, "location": true, "filter": true, "limit": true, "page-size": true, "sort-by": true, "flatten": true, "uri": true, "verbosity": true, "organization": true, "folder": true, "account": false, "database": true, "instance": true}

func gcpCommand(value any, read bool, project string) (string, string, error) {
	words, ok := value.([]any)
	if !ok || len(words) < 2 || len(words) > 100 {
		return "", "", callRefused("command must be an array of literal gcloud or bq words")
	}
	argv := make([]string, len(words))
	for i, v := range words {
		s, ok := v.(string)
		if !ok || s == "" || len(s) > 16000 || strings.ContainsAny(s, "\x00\r\n") {
			return "", "", callRefused("invalid command word")
		}
		argv[i] = s
	}
	program := argv[0]
	if program != "gcloud" && program != "bq" {
		return "", "", callRefused("only gcloud and bq commands are supported")
	}
	// Even execute cannot replace the OAuth identity, load credential files, or
	// alter the executor's configuration. No scripts, files or shell operators.
	for _, s := range argv[1:] {
		key, _, _ := strings.Cut(s, "=")
		for _, blocked := range []string{"--access-token-file", "--credential-file-override", "--impersonate-service-account", "--account", "--configuration", "--flags-file", "--log-http", "--flagfile", "--use_gce_service_account", "--service_account"} {
			if key == blocked || strings.HasPrefix(key, blocked+"_") {
				return "", "", callRefused("credential and configuration overrides are not supported")
			}
		}
	}
	if program == "gcloud" {
		if contains([]string{"auth", "billing", "components", "config", "docker", "feedback", "info", "init", "meta", "survey", "alpha", "beta"}, argv[1]) || len(argv) > 2 && argv[1] == "app" && (argv[2] == "deploy" || argv[2] == "instances") {
			return "", "", callRefused("Google's remote Cloud CLI does not support this command")
		}
	}
	if program == "bq" && contains([]string{"init", "pyshell", "shell"}, argv[1]) {
		return "", "", callRefused("Google's remote Cloud CLI does not support this command")
	}
	if read {
		matched := 0
		for n := 2; n <= len(argv); n++ {
			if gcpReadPaths[strings.Join(argv[:n], " ")] {
				matched = n
				break
			}
		}
		if matched == 0 {
			return "", "", callRefused("read refuses this command; execute requires the user's authorization for this task")
		}
		positional := 0
		for _, s := range argv[matched:] {
			if strings.HasPrefix(s, "--") {
				key, val, has := strings.Cut(strings.TrimPrefix(s, "--"), "=")
				if key == "format" && has && val == "json" {
					continue
				}
				if !gcpReadFlags[key] || !has && key != "uri" {
					return "", "", callRefused("read refuses this flag; use reviewed --name=value flags")
				}
			} else if strings.HasPrefix(s, "-") {
				return "", "", callRefused("read requires full flag names")
			} else {
				positional++
			}
		}
		verb := argv[matched-1]
		if verb == "list" && positional != 0 || verb != "list" && positional != 1 {
			return "", "", callRefused("unexpected resource arguments for read command")
		}
	}
	// The execution project must never silently become the resource project
	// or override a billing project explicitly named by the caller.
	hasProject, hasQuota := false, false
	for _, word := range argv {
		key, value, has := strings.Cut(word, "=")
		if !has {
			continue
		}
		if program == "gcloud" && key == "--project" || program == "bq" && key == "--project_id" {
			if !gcpProjectID.MatchString(value) {
				return "", "", callRefused("use an explicit resource project ID")
			}
			hasProject = true
		}
		if program == "gcloud" && key == "--billing-project" || program == "bq" && key == "--quota_project_id" {
			if !gcpProjectID.MatchString(value) {
				return "", "", callRefused("use an explicit billing project ID")
			}
			hasQuota = true
		}
	}
	global := program == "gcloud" && contains([]string{"projects", "organizations", "resource-manager"}, argv[1])
	if !hasProject && !global {
		return "", "", callRefused("name the resource project with --project=ID (gcloud) or --project_id=ID (bq); execution_project only selects MCP billing and quota")
	}
	if global && !hasProject && !hasQuota {
		argv = append(argv, "--billing-project="+project)
	}
	for i, s := range argv {
		argv[i] = shellquote.Word(s)
	}
	name := "run_gcloud_command"
	if program == "bq" {
		name = "run_bq_command"
	}
	return strings.Join(argv, " "), name, nil
}

func invokeGCP(m *Manager, ctx context.Context, r *record, secret string, t Tool, args map[string]any, _ ...*http.Header) ([]byte, error) {
	known := false
	for _, candidate := range gcpTools() {
		if candidate.Name == t.Name && fingerprint(candidate) == fingerprint(t) {
			known = true
		}
	}
	if !known {
		return nil, callRefused("Google Cloud tool changed; refresh the account")
	}
	if r.Account.Endpoint != gcpMCPEndpoint || r.OAuth == nil {
		return nil, errAuth
	}
	if t.Name == "list_projects" {
		token, _ := args["page_token"].(string)
		return m.gcpProjects(ctx, r, secret, token)
	}
	project, _ := args["execution_project"].(string)
	if !gcpProjectID.MatchString(project) {
		return nil, callRefused("choose an execution_project ID from list_projects")
	}
	command, name, err := gcpCommand(args["command"], t.Name == "read", project)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	data, err := m.gcpRPC(ctx, r, secret, project, name, command)
	var failure serviceError
	if errors.As(err, &failure) {
		failure.detail = string(redactValues([]byte(failure.detail), m.sensitiveValues(r, secret)))
		err = failure
	}
	return data, err
}

func gcpSetup(project string) *AccountSetup {
	return &AccountSetup{Message: "Enable Cloud CLI Execution API and billing for " + project + ". Your account needs MCP Tool User and Service Usage Consumer, plus permission for the resources you use.", URL: "https://console.cloud.google.com/apis/library/cloudcli.googleapis.com?project=" + project}
}

func gcpPrerequisite(project string) string {
	return "Enable Cloud CLI Execution API and billing for " + project + ", and ask its administrator for MCP Tool User (roles/mcp.toolUser), Service Usage Consumer (roles/serviceusage.serviceUsageConsumer), and the resource permissions needed for the task. API: https://console.cloud.google.com/apis/library/cloudcli.googleapis.com?project=" + project + "; IAM: https://console.cloud.google.com/iam-admin/iam?project=" + project
}

// Google documents stateless MCP 2026-07-28. Each call is self-describing;
// never replay a command on transport failure or a header/schema error.
func (m *Manager) gcpRPC(ctx context.Context, r *record, secret, project, name, command string) ([]byte, error) {
	params := map[string]any{"name": name, "arguments": map[string]any{"project": "projects/" + project, "command": command}, "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientInfo": map[string]string{"name": "Mewla", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
	req, err := http.NewRequestWithContext(ctx, "POST", gcpMCPEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("x-goog-user-project", project)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", name)
	resp, err := m.clientFor(r).Do(req)
	if err != nil {
		return nil, errors.New("Google Cloud connection failed; the command may already have run. It was not retried. Check resources before retrying")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return nil, errAuth
	}
	if resp.StatusCode == 403 {
		r.Account.Setup = gcpSetup(project)
		return nil, serviceError{"Google Cloud", gcpPrerequisite(project)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, serviceError{"Google Cloud", fmt.Sprintf("HTTP %d; the command was not retried", resp.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResultBytes+1))
	if err != nil {
		return nil, errNetwork
	}
	if len(raw) > MaxResultBytes {
		return nil, errors.New("Google Cloud result exceeds 1 MiB; request fewer results")
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		scanner.Buffer(make([]byte, 4096), MaxResultBytes+1)
		var event []string
		var answer []byte
		consume := func() {
			if len(event) > 0 {
				b := []byte(strings.Join(event, "\n"))
				var candidate struct {
					ID json.RawMessage `json:"id"`
				}
				if json.Unmarshal(b, &candidate) == nil && string(candidate.ID) == "1" {
					answer = b
				}
				event = nil
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				consume()
			} else if strings.HasPrefix(line, "data:") {
				event = append(event, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		consume()
		if scanner.Err() != nil || answer == nil {
			return nil, errors.New("Google Cloud returned an incomplete MCP response; command was not retried")
		}
		raw = answer
	}
	var rpc struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &rpc) != nil || rpc.ID != 1 {
		return nil, errors.New("Google Cloud returned an invalid MCP response; command was not retried")
	}
	if rpc.Error != nil {
		return nil, serviceError{"Google Cloud", cutText(rpc.Error.Message, maxServiceErrorBytes)}
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if len(rpc.Result) == 0 || bytes.Equal(rpc.Result, []byte("null")) || json.Unmarshal(rpc.Result, &result) != nil {
		return nil, errors.New("Google Cloud returned an invalid tool result")
	}
	if result.IsError {
		var parts []string
		for _, c := range result.Content {
			parts = append(parts, c.Text)
		}
		return nil, serviceError{"Google Cloud", cutText(strings.Join(parts, "\n"), maxServiceErrorBytes)}
	}
	// Some command failures use a successful MCP envelope with a nonzero
	// process exit code. Preserve that distinction in the account's activity.
	var structured struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	_ = json.Unmarshal(rpc.Result, &structured)
	candidates := []json.RawMessage{structured.Structured}
	for _, c := range result.Content {
		candidates = append(candidates, json.RawMessage(c.Text))
	}
	for _, candidate := range candidates {
		var output struct {
			Response struct {
				ExitCode json.RawMessage `json:"exitCode"`
				Stderr   string          `json:"stderr"`
			} `json:"response"`
		}
		if json.Unmarshal(candidate, &output) == nil && len(output.Response.ExitCode) > 0 {
			code := strings.Trim(string(output.Response.ExitCode), "\"")
			if code != "0" {
				return nil, serviceError{"Google Cloud", "Command exited with code " + code + ": " + cutText(output.Response.Stderr, maxServiceErrorBytes)}
			}
		}
	}
	r.Account.Setup = nil
	return rpc.Result, nil
}
