package connections

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/modelprofiles"
)

// slackSignInWire answers Slack as the public app's sign-in sees it: the
// account service's code exchange (through the manager's transport), then
// auth.test with the new token. Each exchange hands out a new token with the
// scopes it was asked for, for the workspace team.
type slackSignInWire struct {
	scopes string
	team   string
	n      int
}

func (w *slackSignInWire) RoundTrip(req *http.Request) (*http.Response, error) {
	answer := func(body string, headers ...string) (*http.Response, error) {
		h := http.Header{"Content-Type": {"application/json"}}
		for i := 0; i+1 < len(headers); i += 2 {
			h.Set(headers[i], headers[i+1])
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	team := w.team
	if team == "" {
		team = "Team"
	}
	switch req.URL.Host + req.URL.Path {
	case "slack.com/api/oauth.v2.access":
		w.n++
		return answer(`{"ok":true,"team":{"id":"T1","name":"` + team + `"},"authed_user":{"id":"U1","scope":"` + w.scopes + `","access_token":"xoxe.xoxp-token-` + itoa(w.n) + `","token_type":"user","refresh_token":"xoxe-1-refresh-` + itoa(w.n) + `","expires_in":43200}}`)
	case "slack.com/api/auth.test":
		return answer(`{"ok":true,"team":"`+team+`","user":"me","user_id":"U1","team_id":"T1"}`, "X-OAuth-Scopes", w.scopes)
	case "slack.com/api/auth.revoke":
		return answer(`{"ok":true,"revoked":true}`)
	}
	return answer(`{"ok":false,"error":"unexpected"}`)
}

const oldSlackScopes = "channels:read,channels:history,search:read,chat:write"

func newSlackScopes() string {
	return strings.Join(append(append([]string(nil), slackReadScopes...), "chat:write"), ",")
}

// slackSignIn runs the app's Slack sign-in end to end: connect_start, then
// the relay's return to mewla://plugins as connect_finish.
func slackSignIn(t *testing.T, m *Manager, in *Input) ConnectFlow {
	t.Helper()
	start := mustHandle(t, m, Request{Action: "connect_start", Input: in}).Flow
	authorize, _ := url.Parse(start.AuthorizationURL)
	state := authorize.Query().Get("state")
	finish := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID, Callback: NativeCallback + "?" + url.Values{"code": {"c"}, "state": {state}}.Encode()}).Flow
	if finish.Status != "connected" {
		t.Fatalf("sign-in %+v", finish)
	}
	return *finish
}

func slackAccountIDs(m *Manager) []string {
	var out []string
	for id, r := range m.records {
		if r.Account.Integration == "slack" {
			out = append(out, id)
		}
	}
	return out
}

func publicSlack(t *testing.T, dir string) (*Manager, *slackSignInWire) {
	t.Helper()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	signedInSlack(m)
	wire := &slackSignInWire{scopes: oldSlackScopes}
	m.networkOwned = false
	m.http = &http.Client{Transport: wire}
	return m, wire
}

func withPublicSlack(t *testing.T) {
	previous := SlackPublicClientID
	SlackPublicClientID = "public-slack"
	t.Cleanup(func() { SlackPublicClientID = previous })
}

// Reconnect from a card renews that account: same ID, name, switch and the
// person's per-tool choices; tools new since the last sign-in follow their
// group's switch.
func TestSlackReconnectRenewsTheSameAccount(t *testing.T) {
	withPublicSlack(t)
	m, wire := publicSlack(t, t.TempDir())
	id := slackSignIn(t, m, &Input{Integration: "slack", AllowWrites: true}).AccountID
	// The person blocked one read and renamed nothing (Slack names cards by identity).
	mustHandle(t, m, Request{Action: "policy", ID: id, Tool: "search_messages", Allowed: false})
	if !m.projection(m.records[id]).Access.ReadConsent {
		t.Fatal("an old-scope sign-in should ask for Reconnect")
	}
	wire.scopes = newSlackScopes()
	again := slackSignIn(t, m, &Input{Integration: "slack", AccountID: id, AllowWrites: true})
	if ids := slackAccountIDs(m); again.AccountID != id || len(ids) != 1 || ids[0] != id {
		t.Fatalf("reconnect made %v (flow account %s, was %s)", ids, again.AccountID, id)
	}
	a := m.projection(m.records[id])
	allowed := map[string]bool{}
	for _, tool := range a.Tools {
		allowed[tool.Name] = tool.Allowed
	}
	if a.Access.ReadConsent || allowed["search_messages"] || !allowed["thread_replies"] || !allowed["get_file"] || !allowed["send_message"] || !allowed["channel_history"] {
		t.Fatalf("after reconnect: consent %v, tools %v", a.Access.ReadConsent, allowed)
	}
}

// What happened on 11-19: the old cards were disconnected before the new
// sign-in finished, and the sign-in skipped them and made new cards. Now the
// same identity revives the card with its ID.
func TestSlackSignInAfterDisconnectKeepsTheID(t *testing.T) {
	withPublicSlack(t)
	m, wire := publicSlack(t, t.TempDir())
	id := slackSignIn(t, m, &Input{Integration: "slack"}).AccountID
	mustHandle(t, m, Request{Action: "disconnect", ID: id})
	wire.scopes = newSlackScopes()
	again := slackSignIn(t, m, &Input{Integration: "slack"})
	r := m.records[id]
	if ids := slackAccountIDs(m); again.AccountID != id || len(ids) != 1 || r.Account.Status != "connected" || !r.Account.Enabled {
		t.Fatalf("sign-in after disconnect: accounts %v, flow %s, was %s", ids, again.AccountID, id)
	}
	if !m.projection(r).Tools[0].Allowed {
		t.Fatal("a revived card reads again")
	}
}

// Reconnect on one workspace's card that signs in to another workspace adds
// that account and leaves the card as it was.
func TestSlackReconnectToAnotherWorkspaceAddsIt(t *testing.T) {
	withPublicSlack(t)
	m, wire := publicSlack(t, t.TempDir())
	id := slackSignIn(t, m, &Input{Integration: "slack"}).AccountID
	wire.team = "Other"
	other := slackSignIn(t, m, &Input{Integration: "slack", AccountID: id}).AccountID
	if other == id || m.records[id].Account.Identity != "Team · me" || m.records[other].Account.Identity != "Other · me" {
		t.Fatalf("cards %s %q, %s %q", id, m.records[id].Account.Identity, other, m.records[other].Account.Identity)
	}
}

// An upgrade folds a disconnected twin left by an earlier Reconnect into the
// live account; the twin's ID keeps working for every call.
func TestStartFoldsADisconnectedTwinIntoTheLiveAccount(t *testing.T) {
	withPublicSlack(t)
	dir := t.TempDir()
	m, wire := publicSlack(t, dir)
	old := slackSignIn(t, m, &Input{Integration: "slack"}).AccountID
	// The 11-19 state: old card disconnected, a new card for the same account.
	mustHandle(t, m, Request{Action: "disconnect", ID: old})
	m.records[old].Account.Status = "disconnected"
	wire.scopes = newSlackScopes()
	live := &record{Account: Account{ID: "live", Integration: "slack", Name: "Team · me", Identity: "Team · me", Endpoint: "https://slack.com/api", AuthMethod: "oauth", Enabled: true, Status: "connected", Scopes: strings.Split(wire.scopes, ","), Tools: slackTools(), History: []Event{}}, OAuth: m.records[old].OAuth, Grants: map[string]string{}}
	for _, tool := range slackTools() {
		live.Grants[tool.Name] = fingerprint(tool)
	}
	m.records["live"] = live
	// Another account's disconnected card, and a twin whose removal never finished, stay.
	m.records["elsewhere"] = &record{Account: Account{ID: "elsewhere", Integration: "slack", Identity: "Other · me", Status: "disconnected", History: []Event{}}, Grants: map[string]string{}}
	m.records["pending"] = &record{Account: Account{ID: "pending", Integration: "slack", Identity: "Team · me", Status: "disconnected", CredentialRemovalPending: true, History: []Event{}}, Grants: map[string]string{}}
	// A Notion identity is only the workspace: two integrations there are not one account.
	m.records["notion-live"] = &record{Account: Account{ID: "notion-live", Integration: "notion", Identity: "Workspace", Status: "connected", Enabled: true, History: []Event{}}, Grants: map[string]string{}}
	m.records["notion-other"] = &record{Account: Account{ID: "notion-other", Integration: "notion", Identity: "Workspace", Status: "disconnected", History: []Event{}}, Grants: map[string]string{}}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}

	vault, _ := modelprofiles.NewFileCredentialStore(filepath.Join(dir, "integration-credentials.json"))
	restarted, err := newManager(filepath.Join(dir, "integrations.json"), vault, &http.Client{Transport: wire})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.records[old]; ok {
		t.Fatal("the twin is still there")
	}
	if restarted.records["elsewhere"] == nil || restarted.records["pending"] == nil || restarted.records["notion-other"] == nil || !contains(restarted.records["live"].Aliases, old) {
		t.Fatalf("records %v, aliases %v", slackAccountIDs(restarted), restarted.records["live"].Aliases)
	}
	// Saved: a second start finds nothing to fold.
	raw, _ := json.Marshal(restarted.records["live"].Aliases)
	if !strings.Contains(string(raw), old) {
		t.Fatal("aliases not saved")
	}
	// The old ID still reaches the account, for the app, agents and readers.
	if got := mustHandle(t, restarted, Request{Action: "get", ID: old}).Account; got.ID != "live" {
		t.Fatalf("get by old id: %+v", got)
	}
	if matches := mustHandle(t, restarted, Request{Action: "describe", ID: old}).Matches; len(matches) == 0 || matches[0].ConnectionID != "live" {
		t.Fatalf("describe by old id: %v", matches)
	}
	if err := restarted.vault.Set("integration:live", `{"token":{"access_token":"xoxe.xoxp-token-2","token_type":"Bearer","expiry":"2099-01-01T00:00:00Z"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ReadTool(context.Background(), old, "get_me", map[string]any{}); err != nil {
		t.Fatalf("read by old id: %v", err)
	}
}

// slackSignInGranting runs the app's sign-in where Slack grants grant(asked):
// asked is the user_scope the authorization URL carried, as Slack sees it.
func slackSignInGranting(t *testing.T, m *Manager, wire *slackSignInWire, in *Input, grant func(asked []string) []string) (string, []string) {
	t.Helper()
	start := mustHandle(t, m, Request{Action: "connect_start", Input: in}).Flow
	authorize, _ := url.Parse(start.AuthorizationURL)
	asked := strings.Split(authorize.Query().Get("user_scope"), ",")
	wire.scopes = strings.Join(grant(asked), ",")
	finish := mustHandle(t, m, Request{Action: "connect_finish", FlowID: start.ID, Callback: NativeCallback + "?" + url.Values{"code": {"c"}, "state": {authorize.Query().Get("state")}}.Encode()}).Flow
	if finish.Status != "connected" {
		t.Fatalf("sign-in %+v", finish)
	}
	return finish.AccountID, asked
}

// What the app sends from a card's Reconnect (core reconnectInput): the card
// names itself and asks for changes again when it allows them.
func appReconnect(a Account) *Input {
	writes := a.Access != nil && (a.Access.Write == "allowed" || a.Access.Write == "partial")
	return &Input{Integration: a.Integration, AccountID: a.ID, AllowWrites: writes}
}

// 11-27: a card that sends messages and lacks files:write (signed in before
// send_files) runs the app's Reconnect: it asks Slack for files:write and keeps
// what Slack granted, so send_files comes and the card stops asking.
func TestSlackReconnectAddsALaterChangeScope(t *testing.T) {
	withPublicSlack(t)
	m, wire := publicSlack(t, t.TempDir())
	before := append(append([]string(nil), slackReadScopes...), "chat:write")
	id, _ := slackSignInGranting(t, m, wire, &Input{Integration: "slack", AllowWrites: true}, func([]string) []string { return before })
	card := m.projection(m.records[id])
	if !card.Access.WriteUpgrade || card.Access.Write != "allowed" {
		t.Fatalf("old card access %+v", card.Access)
	}
	_, asked := slackSignInGranting(t, m, wire, appReconnect(card), func(asked []string) []string { return asked })
	if !contains(asked, "files:write") || !contains(asked, "chat:write") {
		t.Fatalf("Reconnect asked Slack for %v", asked)
	}
	r := m.records[id]
	a := m.projection(r)
	if ids := slackAccountIDs(m); len(ids) != 1 || !contains(r.Account.Scopes, "files:write") || a.Access.WriteUpgrade || len(a.DeclinedScopes) != 0 {
		t.Fatalf("after Reconnect: accounts %v, scopes %v, access %+v, declined %v", ids, r.Account.Scopes, a.Access, a.DeclinedScopes)
	}
	if allowed := toolAllowed(a); !allowed["send_files"] || !allowed["send_message"] {
		t.Fatalf("tools after Reconnect %v", allowed)
	}
}

// A provider that grants less than the sign-in asked for: the card keeps what
// was granted, names what was not, and the sign-in says so, instead of a quiet
// success that offers the same Reconnect again.
func TestSignInGrantedLessThanAskedNamesTheMissingScopes(t *testing.T) {
	withPublicSlack(t)
	m, wire := publicSlack(t, t.TempDir())
	without := func(asked []string) []string {
		var out []string
		for _, s := range asked {
			if s != "files:write" {
				out = append(out, s)
			}
		}
		return out
	}
	id, _ := slackSignInGranting(t, m, wire, &Input{Integration: "slack", AllowWrites: true}, without)
	r := m.records[id]
	a := m.projection(r)
	if contains(r.Account.Scopes, "files:write") || !a.Access.WriteUpgrade || strings.Join(a.DeclinedScopes, ",") != "files:write" {
		t.Fatalf("scopes %v, access %+v, declined %v", r.Account.Scopes, a.Access, a.DeclinedScopes)
	}
	if h := a.History[0]; h.Tool != "authorize" || !strings.Contains(h.Message, "files:write") {
		t.Fatalf("history %+v", h)
	}
	// The next sign-in that gets everything clears it.
	slackSignInGranting(t, m, wire, appReconnect(a), func(asked []string) []string { return asked })
	if a = m.projection(m.records[id]); len(a.DeclinedScopes) != 0 || a.Access.WriteUpgrade {
		t.Fatalf("a full grant still names %v (access %+v)", a.DeclinedScopes, a.Access)
	}
}

func toolAllowed(a Account) map[string]bool {
	out := map[string]bool{}
	for _, tool := range a.Tools {
		out[tool.Name] = tool.Allowed
	}
	return out
}
