package connections

import "testing"

// The projected access summary drives Plugins: Linear's save_* tools are its
// changes, and a read-only Linear grant needs the service to ask again.
func TestAccessSummary(t *testing.T) {
	tools := []Tool{{Name: "list_issues"}, {Name: "get_team"}, {Name: "save_issue"}, {Name: "extract_images"}}
	r := &record{Account: Account{Integration: "linear", Endpoint: "https://mcp.linear.app/mcp", AuthMethod: "mcp_oauth", Scopes: []string{"read", "openid", "email"}, Tools: tools}, OAuth: &oauthAccount{}, Grants: map[string]string{}}
	r.Grants["list_issues"] = fingerprint(tools[0])
	a := (&Manager{}).projection(r).Access
	if a.Read != "partial" || a.Write != "off" || !a.WriteConsent || a.Allowed != 1 || a.Tools != 4 {
		t.Fatalf("read-only Linear access %+v", a)
	}
	r.Account.Scopes = append(r.Account.Scopes, "write")
	(&Manager{}).setGroup(r, "read", true)
	(&Manager{}).setGroup(r, "write", true)
	if a := (&Manager{}).projection(r).Access; a.Read != "allowed" || a.Write != "allowed" || a.WriteConsent {
		t.Fatalf("read-write Linear access %+v", a)
	}
	custom := &record{Account: Account{Integration: "mcp", Tools: tools}, OAuth: &oauthAccount{}}
	if a := (&Manager{}).projection(custom).Access; a.Read != "none" || a.Write != "none" || a.WriteConsent {
		t.Fatalf("custom access %+v", a)
	}
	slack := &record{Account: Account{Integration: "slack", Scopes: []string{"channels:read"}}, OAuth: &oauthAccount{}}
	google := &record{Account: Account{Integration: "google", Scopes: googleScopes(false)}, OAuth: &oauthAccount{}}
	token := &record{Account: Account{Integration: "slack"}}
	if !writeConsent(slack) || !writeConsent(google) || writeConsent(token) {
		t.Fatal("write consent misreads Slack, Google or a token account")
	}
	google.Account.Scopes = googleScopes(true)
	if writeConsent(google) {
		t.Fatal("Google with write scopes still asks again")
	}
}
