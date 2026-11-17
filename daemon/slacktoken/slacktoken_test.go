package slacktoken_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/remote"
	"github.com/daoleno/mewla/daemon/slacktoken"
)

const relay = "https://connect.example/plugins/return"

type accounts map[string]string

func (a accounts) AccountForKey(_ context.Context, key string) (string, bool, error) {
	id, ok := a[key]
	return id, ok, nil
}

// fakeSlack answers oauth.v2.access and records what it was sent.
type fakeSlack struct {
	forms []url.Values
	auths []string
	down  bool
}

func (f *fakeSlack) server(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = r.ParseForm()
		f.forms = append(f.forms, r.PostForm)
		id, secret, _ := r.BasicAuth()
		f.auths = append(f.auths, id+":"+secret)
		if secret != "app-secret" {
			_, _ = io.WriteString(w, `{"ok":false,"error":"bad_client_secret"}`)
			return
		}
		if r.PostForm.Get("grant_type") == "refresh_token" {
			_, _ = io.WriteString(w, `{"ok":true,"access_token":"xoxe.xoxp-new","refresh_token":"xoxe-new","expires_in":43200}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"authed_user":{"access_token":"xoxe.xoxp-1","refresh_token":"xoxe-1","expires_in":43200,"scope":"search:read"}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

type rig struct {
	slack   *fakeSlack
	service *httptest.Server
	client  *remote.Client
}

func newRig(t *testing.T, config slacktoken.Config, signedIn bool) rig {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	slack := &fakeSlack{}
	config.TokenURL = slack.server(t).URL
	config.RedirectURI = relay
	known := accounts{}
	if signedIn {
		known[manager.PublicKeyHex()] = "acct-1"
	}
	config.Accounts = known
	service := httptest.NewServer(slacktoken.New(config).Handler())
	t.Cleanup(service.Close)
	client, err := remote.NewClient(service.URL, "", manager)
	if err != nil {
		t.Fatal(err)
	}
	return rig{slack: slack, service: service, client: client}
}

func ready() slacktoken.Config {
	return slacktoken.Config{ClientID: "app-id", ClientSecret: "app-secret"}
}

func TestExchangeAndRefreshCarryTheAppsSecret(t *testing.T) {
	r := newRig(t, ready(), true)
	raw, err := r.client.SlackToken(context.Background(), slacktoken.ExchangePath, slacktoken.Exchange{Code: "code", CodeVerifier: "verifier", RedirectURI: relay})
	if err != nil || !strings.Contains(string(raw), `"authed_user"`) {
		t.Fatalf("exchange = %s, %v", raw, err)
	}
	raw, err = r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "xoxe-1"})
	if err != nil || !strings.Contains(string(raw), `"xoxe-new"`) {
		t.Fatalf("refresh = %s, %v", raw, err)
	}
	if len(r.slack.forms) != 2 {
		t.Fatalf("Slack saw %d requests", len(r.slack.forms))
	}
	exchange, refresh := r.slack.forms[0], r.slack.forms[1]
	if exchange.Get("grant_type") != "authorization_code" || exchange.Get("code") != "code" || exchange.Get("code_verifier") != "verifier" || exchange.Get("redirect_uri") != relay {
		t.Fatalf("exchange form = %v", exchange)
	}
	if refresh.Get("grant_type") != "refresh_token" || refresh.Get("refresh_token") != "xoxe-1" {
		t.Fatalf("refresh form = %v", refresh)
	}
	for i, form := range r.slack.forms {
		// The secret goes in Basic auth only, never in the form.
		if r.slack.auths[i] != "app-id:app-secret" || form.Has("client_secret") || form.Has("client_id") {
			t.Fatalf("request %d: basic auth sent %v, form %v", i, r.slack.auths[i] != ":", form)
		}
	}
}

// Slack's own refusal comes back as Slack said it, for the daemon to read.
func TestSlackRefusalPassesThrough(t *testing.T) {
	config := ready()
	config.ClientSecret = "wrong"
	r := newRig(t, config, true)
	raw, err := r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "xoxe-1"})
	if err != nil || !strings.Contains(string(raw), `"bad_client_secret"`) {
		t.Fatalf("refusal = %s, %v", raw, err)
	}
}

func TestOnlySignedInComputersAndFixedReturn(t *testing.T) {
	r := newRig(t, ready(), false)
	if _, err := r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "x"}); !errors.Is(err, remote.ErrSignedOut) {
		t.Fatalf("unknown computer = %v", err)
	}
	unsigned, err := http.Post(r.service.URL+slacktoken.RefreshPath, "application/json", strings.NewReader(`{"refresh_token":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	unsigned.Body.Close()
	if unsigned.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned = %d", unsigned.StatusCode)
	}
	r = newRig(t, ready(), true)
	for name, body := range map[string]any{
		"other return":  slacktoken.Exchange{Code: "c", CodeVerifier: "v", RedirectURI: "https://evil.example/cb"},
		"no verifier":   slacktoken.Exchange{Code: "c", RedirectURI: relay},
		"extra field":   map[string]string{"code": "c", "code_verifier": "v", "redirect_uri": relay, "client_id": "other"},
		"empty refresh": map[string]string{},
	} {
		path := slacktoken.ExchangePath
		if name == "empty refresh" {
			path = slacktoken.RefreshPath
		}
		var service *remote.ServiceError
		if _, err := r.client.SlackToken(context.Background(), path, body); !errors.As(err, &service) || service.Status != http.StatusBadRequest {
			t.Fatalf("%s = %v", name, err)
		}
	}
	if len(r.slack.forms) != 0 {
		t.Fatal("a refused request reached Slack")
	}
}

// Without Mewla's Slack app configured the service still runs; Slack answers
// slack_unavailable, never a sign-out.
func TestUnconfiguredSlackIsUnavailable(t *testing.T) {
	r := newRig(t, slacktoken.Config{}, true)
	var service *remote.ServiceError
	if _, err := r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "x"}); !errors.As(err, &service) || service.Status != http.StatusServiceUnavailable || service.Code != "slack_unavailable" {
		t.Fatalf("unconfigured = %v", err)
	}
}

func TestSlackDownIsBadGateway(t *testing.T) {
	r := newRig(t, ready(), true)
	r.slack.down = true
	var service *remote.ServiceError
	if _, err := r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "x"}); !errors.As(err, &service) || service.Status != http.StatusBadGateway {
		t.Fatalf("Slack down = %v", err)
	}
}

func TestRefreshesAreRateLimitedPerAccount(t *testing.T) {
	r := newRig(t, ready(), true)
	for i := 0; i < 120; i++ {
		if _, err := r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "x"}); err != nil {
			t.Fatalf("refresh %d = %v", i, err)
		}
	}
	var service *remote.ServiceError
	if _, err := r.client.SlackToken(context.Background(), slacktoken.RefreshPath, slacktoken.Refresh{RefreshToken: "x"}); !errors.As(err, &service) || service.Status != http.StatusTooManyRequests {
		t.Fatalf("121st refresh = %v", err)
	}
}
