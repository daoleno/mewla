package connections

// Reviewed names for fixed, official resources. Remote readOnlyHint annotations
// never grant anything. Grants still fingerprint the discovered full definition,
// and invocation rechecks it. Unknown/new tools require individual review.
var officialReads = map[string]map[string]bool{
	"notion": names("notion-search", "notion-fetch", "notion-get-comments", "notion-get-users", "notion-get-self", "notion-get-teams"),
	"linear": names("get_user", "list_users", "get_issue", "list_issues", "get_project", "list_projects", "list_teams", "get_team", "list_comments", "list_cycles", "list_issue_statuses", "get_issue_status", "list_issue_labels", "get_document", "list_documents", "get_project_status", "list_project_statuses"),
}
var officialWrites = map[string]map[string]bool{
	"notion": names("notion-create-pages", "notion-update-page", "notion-create-comment", "notion-move-pages", "notion-duplicate-page"),
	"linear": names("create_issue", "update_issue", "create_comment", "update_comment", "create_project", "update_project", "create_document", "update_document"),
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
		expected := map[string]string{"notion": "https://mcp.notion.com/mcp", "linear": "https://mcp.linear.app/mcp"}[r.Account.Integration]
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
	if r.Account.Integration == "google" {
		for _, op := range googleOperations() {
			reviewed = append(reviewed, op.tool)
		}
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
		}
	}
	return true
}
