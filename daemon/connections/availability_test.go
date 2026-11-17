package connections

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogReportsPublisherReadiness(t *testing.T) {
	oldGitHub, oldSlack := GitHubPublicClientID, SlackPublicClientID
	GitHubPublicClientID, SlackPublicClientID = "", ""
	defer func() {
		GitHubPublicClientID, SlackPublicClientID = oldGitHub, oldSlack
	}()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assertAvailability := func(expected map[string]bool) {
		t.Helper()
		result := mustHandle(t, m, Request{Action: "list"})
		if len(result.Catalog) != len(expected) {
			t.Fatalf("catalog: %+v", result.Catalog)
		}
		for _, item := range result.Catalog {
			available, listed := expected[item.ID]
			if !listed || item.Available != available || (item.UnavailableReason == "") != item.Available {
				t.Fatalf("incorrect readiness: %+v", item)
			}
		}
	}
	expected := map[string]bool{"github": false, "slack": false, "notion": true, "linear": true, "onlora": true, "cloudflare": true, "aws": true, "gcp": true, "gws": true, "tailscale": true, "hyperliquid": true, "mcp": true, "openapi": true}
	assertAvailability(expected)
	for _, kind := range []string{"github", "slack"} {
		_, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: kind}})
		if err == nil || !strings.Contains(err.Error(), "sign-in is not set up yet") {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if len(m.pending) != 0 || len(m.connectFlows) != 0 || len(m.records) != 0 {
		t.Fatal("unavailable connection created state")
	}
	GitHubPublicClientID, SlackPublicClientID = "public-github", "public-slack"
	// Mewla's public Slack app renews through the account service, so it
	// needs this computer signed in to a Mewla account.
	expected["github"] = true
	assertAvailability(expected)
	if reason := m.unavailableReason("slack"); reason != "Sign in to your Mewla account to connect Slack." {
		t.Fatalf("signed-out Slack: %q", reason)
	}
	signedInSlack(m)
	expected["slack"] = true
	m.records["existing-github"] = &record{Account: Account{ID: "existing-github", Integration: "github", Enabled: true, Status: "connected"}}
	assertAvailability(expected)
	GitHubPublicClientID, SlackPublicClientID = "", ""
	for _, kind := range []string{"github", "slack"} {
		mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: kind, OAuthClient: &OAuthClientConfig{ClientID: "operator", ClientSecret: "private", RedirectURL: "https://daemon.example/plugins/oauth/callback"}}})
	}
	assertAvailability(expected)
	// Existing linked accounts remain accessible when new sign-in is unavailable.
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 1 {
		t.Fatal("existing account hidden")
	}
}
