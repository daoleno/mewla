package connections

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogReportsPublisherReadiness(t *testing.T) {
	oldGitHub, oldSlack, oldGoogle := GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "", "", ""
	defer func() {
		GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = oldGitHub, oldSlack, oldGoogle
	}()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assertAvailability := func(expected map[string]bool) {
		t.Helper()
		result := mustHandle(t, m, Request{Action: "list"})
		if len(result.Catalog) != 7 {
			t.Fatalf("catalog count: %d", len(result.Catalog))
		}
		for _, item := range result.Catalog {
			if item.Available != expected[item.ID] || (item.UnavailableReason == "") != item.Available {
				t.Fatalf("incorrect readiness: %+v", item)
			}
		}
	}
	expected := map[string]bool{"github": false, "slack": false, "google": false, "notion": true, "linear": true, "mcp": true, "openapi": true}
	assertAvailability(expected)
	for _, kind := range []string{"github", "slack", "google"} {
		_, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: kind}})
		if err == nil || !strings.Contains(err.Error(), "sign-in is not set up yet") {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if len(m.pending) != 0 || len(m.connectFlows) != 0 || len(m.records) != 0 {
		t.Fatal("unavailable connection created state")
	}
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "public-github", "public-slack", "https://publisher.example"
	expected["github"], expected["slack"], expected["google"] = true, true, true
	assertAvailability(expected)
	GitHubPublicClientID, SlackPublicClientID, GoogleExchangeOrigin = "", "", ""
	for _, kind := range []string{"github", "slack", "google"} {
		mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: kind, OAuthClient: &OAuthClientConfig{ClientID: "operator", ClientSecret: "private", RedirectURL: "https://daemon.example/plugins/oauth/callback"}}})
	}
	assertAvailability(expected)
	// Incomplete saved configuration must not offer a connection that cannot exchange a code.
	if err := m.vault.Set("oauth-client:google", `{"client_id":"operator","redirect_url":"https://daemon.example/plugins/oauth/callback"}`); err != nil {
		t.Fatal(err)
	}
	expected["google"] = false
	assertAvailability(expected)
	// Existing linked accounts remain accessible when new sign-in is unavailable.
	m.records["existing"] = &record{Account: Account{ID: "existing", Integration: "google", Enabled: true, Status: "connected"}}
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 1 {
		t.Fatal("existing account hidden")
	}
}
