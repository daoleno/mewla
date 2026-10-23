package connections

import (
	"errors"
	"fmt"
)

// Reviewed names for fixed, official resources. Remote readOnlyHint annotations
// never grant anything. Grants still fingerprint the discovered full definition,
// and invocation rechecks it. Unknown/new tools require individual review.
var officialReads = map[string]map[string]bool{
	"notion":     names("notion-search", "notion-fetch", "notion-get-comments", "notion-get-users", "notion-get-self", "notion-get-teams"),
	"linear":     names("get_user", "list_users", "get_issue", "list_issues", "get_project", "list_projects", "list_teams", "get_team", "list_comments", "list_cycles", "list_issue_statuses", "get_issue_status", "list_issue_labels", "get_document", "list_documents", "get_project_status", "list_project_statuses", "list_milestones", "get_milestone", "list_project_labels", "get_status_updates"),
	"cloudflare": names("search", "docs", "whoami"),
	"onlora":     names("list_projects", "get_project", "list_files", "read_file", "search_library", "get_asset", "get_job", "get_quota", "list_models"),
}
var officialWrites = map[string]map[string]bool{
	"notion": names("notion-create-pages", "notion-update-page", "notion-create-comment", "notion-move-pages", "notion-duplicate-page"),
	// Linear's MCP server now saves (creates or updates) through save_* tools.
	"linear": names("create_issue", "update_issue", "create_comment", "update_comment", "create_project", "update_project", "create_document", "update_document", "save_issue", "save_comment", "save_project", "save_document", "save_milestone", "save_status_update"),
	// execute runs any Cloudflare API call the grant allows, changes included.
	"cloudflare": names("execute"),
	// generate_image, edit_image and the paid generate_* tools spend the
	// account's quota (generate_voice is free unless a paid model is named);
	// delete_file removes a project file; update_asset changes an asset's
	// title or note.
	"onlora": names("create_project", "update_project", "write_file", "delete_file", "add_asset", "update_asset", "create_upload", "generate_image", "edit_image", "generate_voice"),
}

func names(values ...string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		out[v] = true
	}
	return out
}
func toolGroup(r *record, t Tool) string {
	if r.Account.Integration == "mcp" || r.Account.Integration == "openapi" {
		return ""
	}
	if r.Account.AuthMethod == "mcp_oauth" || r.Account.Integration == "linear" {
		expected := map[string]string{"notion": "https://mcp.notion.com/mcp", "linear": "https://mcp.linear.app/mcp", "onlora": onloraMCP, "cloudflare": cloudflareMCP}[r.Account.Integration]
		if expected == "" || r.Account.Endpoint != expected {
			return ""
		}
		if officialReads[r.Account.Integration][t.Name] {
			return "read"
		}
		if officialWrites[r.Account.Integration][t.Name] {
			return "write"
		}
		return ""
	}
	var reviewed []Tool
	if isGoogle(r.Account.Integration) {
		reviewed = googleReviewedTools(r.Account.Integration)
	} else {
		reviewed = builtinTools(r.Account.Integration)
	}
	for _, candidate := range reviewed {
		if fingerprint(candidate) == fingerprint(t) {
			if candidate.Read {
				return "read"
			}
			return "write"
		}
	}
	return ""
}
func (m *Manager) setGroup(r *record, group string, allowed bool) bool {
	if (group != "read" && group != "write") || r.Account.Integration == "mcp" || r.Account.Integration == "openapi" {
		return false
	}
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	for _, t := range r.Account.Tools {
		if toolGroup(r, t) == group {
			if allowed {
				r.Grants[t.Name] = fingerprint(t)
			} else {
				delete(r.Grants, t.Name)
			}
			clearUpdates(r, t.Name)
		}
	}
	return true
}

// setPolicy allows or blocks one tool, or every tool in a batch. A batch names
// the tools the user saw, so a tool discovered later is never granted by it,
// and it applies all of them or none.
func setPolicy(r *record, q Request) error {
	names := q.Tools
	if q.Tool != "" {
		if len(names) != 0 {
			return errors.New("name one tool or a list of tools, not both")
		}
		names = []string{q.Tool}
	}
	if len(names) == 0 {
		return errors.New("tool not found")
	}
	tools := map[string]Tool{}
	for _, t := range r.Account.Tools {
		tools[t.Name] = t
	}
	for _, name := range names {
		if _, ok := tools[name]; !ok {
			return fmt.Errorf("tool not found: %s", name)
		}
	}
	if r.Grants == nil {
		r.Grants = map[string]string{}
	}
	for _, name := range names {
		if q.Allowed {
			r.Grants[name] = fingerprint(tools[name])
		} else {
			delete(r.Grants, name)
		}
		clearUpdates(r, name)
	}
	return nil
}

func groupState(tools []Tool, group string) string {
	total, allowed := 0, 0
	for _, t := range tools {
		if t.Group == group {
			total++
			if t.Allowed {
				allowed++
			}
		}
	}
	switch {
	case total == 0:
		return "none"
	case allowed == total:
		return "allowed"
	case allowed == 0:
		return "off"
	}
	return "partial"
}

// writeConsent reports a sign-in whose grant lacks the service's write scope,
// so allowing changes means connecting again with writes. Tokens and imported
// logins carry whatever the person granted them.
func writeConsent(r *record) bool {
	if r.OAuth == nil {
		return false
	}
	switch r.Account.Integration {
	case "slack":
		return !contains(r.Account.Scopes, "chat:write")
	case "linear":
		return !contains(r.Account.Scopes, "write")
	case googleCalendarID, gmailID:
		read := googleScopes(r.Account.Integration, false)
		for _, scope := range googleScopes(r.Account.Integration, true) {
			if !contains(read, scope) && !contains(r.Account.Scopes, scope) {
				return true
			}
		}
	}
	return false
}
