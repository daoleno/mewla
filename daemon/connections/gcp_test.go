package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestGCPCommandsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		words    []string
		read, ok bool
	}{
		{[]string{"gcloud", "compute", "instances", "list", "--project=resource-project", "--format=json"}, true, true},
		{[]string{"gcloud", "projects", "get-iam-policy", "my-project"}, true, true},
		{[]string{"gcloud", "compute", "instances", "describe", "one", "--zone=us-east1-a", "--project=my-project"}, true, true},
		{[]string{"gcloud", "compute", "instances", "delete", "list"}, true, false},
		{[]string{"gcloud", "compute", "instances", "list", "--impersonate-service-account=admin"}, true, false},
		{[]string{"gcloud", "compute", "instances", "list", "--flags-file=evil"}, true, false},
		{[]string{"gcloud", "compute", "instances", "list", "--format=json;delete"}, true, false},
		{[]string{"gcloud", "compute", "instances", "list", "--log-http"}, true, false},
		{[]string{"gcloud", "compute", "instances", "list", ";", "gcloud", "projects", "delete", "p"}, true, false},
		{[]string{"gcloud", "secrets", "versions", "access", "latest"}, true, false},
		{[]string{"gcloud", "container", "clusters", "get-credentials", "cluster"}, true, false},
		{[]string{"bq", "query", "SELECT 1"}, true, false},
		{[]string{"bq", "query", "SELECT * FROM `p.d.t` LIMIT 1", "--project_id=my-project"}, false, true},
		{[]string{"gcloud", "compute", "instances", "delete", "one", "--project=my-project"}, false, true},
		{[]string{"gcloud", "compute", "instances", "delete", "one"}, false, false},
		{[]string{"gcloud", "auth", "print-access-token"}, false, false},
		{[]string{"sh", "-c", "gcloud projects list"}, false, false},
	} {
		args := make([]any, len(tc.words))
		for i, w := range tc.words {
			args[i] = w
		}
		command, _, err := gcpCommand(args, tc.read, "quota-project")
		if (err == nil) != tc.ok {
			t.Errorf("%q read=%v: %s, %v", tc.words, tc.read, command, err)
		}

	}
}

func TestGCPOAuthLoopbackAndPasteLifecycle(t *testing.T) {
	for _, paste := range []bool{false, true} {
		t.Run(map[bool]string{false: "loopback", true: "paste"}[paste], func(t *testing.T) {
			var tokenExchanges, refreshes, mcpCalls int
			var state, challenge string
			m := testManager(t, func(req *http.Request) (*http.Response, error) {
				switch req.URL.Host {
				case "oauth2.googleapis.com":
					_ = req.ParseForm()
					if req.URL.Path == "/revoke" {
						return reply(200, `{}`), nil
					}
					if req.Form.Get("client_id") != "test-client" || req.Form.Get("client_secret") != "test-client-secret" {
						t.Fatal("configured client lost")
					}
					if req.Form.Get("grant_type") == "refresh_token" {
						refreshes++
						if req.Form.Get("refresh_token") != "refresh-test" {
							t.Fatal("refresh token lost")
						}
					} else {
						tokenExchanges++
						if req.Form.Get("redirect_uri") != "http://127.0.0.1:19879/plugins/oauth/callback" || oauth2.S256ChallengeFromVerifier(req.Form.Get("code_verifier")) != challenge {
							t.Fatal("PKCE or redirect mismatch")
						}
					}
					resp := reply(200, `{"access_token":"access-test","refresh_token":"refresh-test","expires_in":3600,"token_type":"Bearer"}`)
					resp.Header.Set("Content-Type", "application/json")
					return resp, nil
				case "www.googleapis.com":
					return reply(200, `{"email":"person@example.com","sub":"person-id"}`), nil
				case "cloudresourcemanager.googleapis.com":
					return reply(200, `{"projects":[{"projectId":"my-project","state":"ACTIVE"}]}`), nil
				case "serviceusage.googleapis.com":
					return reply(200, `{"state":"DISABLED"}`), nil
				case "cloudcli.googleapis.com":
					mcpCalls++
					if req.Header.Get("Authorization") != "Bearer access-test" || req.Header.Get("x-goog-user-project") != "my-project" || req.Header.Get("MCP-Protocol-Version") != "2026-07-28" || req.Header.Get("Mcp-Name") != "run_gcloud_command" {
						t.Fatal("missing authentication or routing headers")
					}
					return reply(200, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[]"}]}}`), nil
				default:
					t.Fatalf("unexpected endpoint %s", req.URL.Host)
					return nil, nil
				}
			})
			m.SetLoopbackPort(19879)
			if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "gcp"}}); err == nil {
				t.Fatal("started without client")
			}
			mustHandle(t, m, Request{Action: "oauth_configure", Input: &Input{Integration: "gcp", OAuthClient: &OAuthClientConfig{ClientID: "test-client", ClientSecret: "test-client-secret"}}})
			flow := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gcp"}}).Flow
			u, _ := url.Parse(flow.AuthorizationURL)
			q := u.Query()
			state = q.Get("state")
			challenge = q.Get("code_challenge")
			if u.Host != "accounts.google.com" || q.Get("client_id") != "test-client" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" || !strings.Contains(q.Get("scope"), gcpScope) || !flow.AddressRequired {
				t.Fatal("incorrect Google authorization request")
			}
			callback := "http://127.0.0.1:19879/plugins/oauth/callback?state=" + state + "&code=code-test"
			if paste {
				mustHandle(t, m, Request{Action: "connect_finish", FlowID: flow.ID, Callback: callback})
			} else {
				w := httptest.NewRecorder()
				m.BrowserOAuthCallback(w, httptest.NewRequest("GET", callback, nil), false)
				if w.Code != 200 {
					t.Fatalf("callback: %s", w.Body.String())
				}
			}
			done := mustHandle(t, m, Request{Action: "connect_status", FlowID: flow.ID})
			if done.Account == nil {
				t.Fatalf("not connected: %+v", done.Flow)
			}
			a := done.Account
			if a.Identity != "person@example.com" || a.Setup == nil || !strings.Contains(a.Setup.URL, "my-project") || a.Access.Read != "allowed" || a.Access.Write == "allowed" {
				t.Fatalf("account: %+v", a)
			}
			args := json.RawMessage(`{"execution_project":"my-project","command":["gcloud","projects","list"]}`)
			if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: "execute", Arguments: args}); err == nil {
				t.Fatal("write without grant")
			}
			if mcpCalls != 0 {
				t.Fatal("unauthorized write reached Google")
			}
			mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "read", Arguments: args})
			mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "write", Allowed: true})
			mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "execute", Arguments: json.RawMessage(`{"execution_project":"my-project","command":["gcloud","compute","instances","stop","one","--project=my-project"]}`)})
			if mcpCalls != 2 {
				t.Fatal("explicit write grant was not honored")
			}
			if m.records[a.ID].Account.Setup != nil {
				t.Fatal("successful call did not clear setup notice")
			}
			r := m.records[a.ID]
			raw, _, _ := m.vault.Get("integration:" + a.ID)
			var saved oauthSecret
			_ = json.Unmarshal([]byte(raw), &saved)
			saved.Token.Expiry = time.Now().Add(-time.Hour)
			data, _ := json.Marshal(saved)
			_ = m.vault.Set("integration:"+a.ID, string(data))
			if _, err := m.accountToken(context.Background(), r); err != nil || refreshes != 1 {
				t.Fatalf("refresh failed: %v", err)
			}
			// A callback is consumed once, including when submitted from another device.
			w := httptest.NewRecorder()
			m.BrowserOAuthCallback(w, httptest.NewRequest("GET", callback, nil), false)
			if w.Code == 200 || tokenExchanges != 1 {
				t.Fatal("callback replay accepted")
			}
			mustHandle(t, m, Request{Action: "disconnect", ID: a.ID})
			if _, ok, _ := m.vault.Get("integration:" + a.ID); ok {
				t.Fatal("disconnect kept credentials")
			}
		})
	}
}

func TestGCPTransportErrorsAndSSE(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		body, contentType string
		network           bool
		want              string
	}{
		{name: "permission", status: 403, want: "roles/mcp.toolUser"},
		{name: "auth", status: 401, want: "authorization"},
		{name: "network", network: true, want: "not retried"},
		{name: "rpc", status: 200, body: `{"id":1,"error":{"code":-1,"message":"rejected"}}`, want: "rejected"},
		{name: "tool", status: 200, body: `{"id":1,"result":{"isError":true,"content":[{"text":"denied"}]}}`, want: "denied"},
		{name: "exit", status: 200, body: `{"id":1,"result":{"structuredContent":{"response":{"exitCode":"1","stderr":"no permission"}},"content":[]}}`, want: "Command exited with code 1"},
		{name: "sse", status: 200, contentType: "text/event-stream", body: "event: message\ndata: {\"id\":1,\"result\":{\"content\":[]}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			m := testManager(t, func(req *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(req.Body)
				if !strings.Contains(string(raw), `"project":"projects/my-project"`) {
					t.Fatal("missing execution project")
				}
				if tc.network {
					return nil, errors.New("broken")
				}
				resp := reply(tc.status, tc.body)
				resp.Header.Set("Content-Type", tc.contentType)
				return resp, nil
			})
			r := &record{Account: Account{Integration: "gcp", Endpoint: gcpMCPEndpoint}}
			_, err := m.gcpRPC(context.Background(), r, "test-access", "my-project", "run_gcloud_command", "gcloud projects list")
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("unexpected: %v", err)
			}
			if calls != 1 {
				t.Fatalf("retried %d times", calls)
			}
		})
	}
}

func TestGCPProjectDiscoveryPagination(t *testing.T) {
	m := testManager(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cloudresourcemanager.googleapis.com" || req.URL.Query().Get("pageToken") != "next +/" {
			t.Fatal("incorrect discovery pagination")
		}
		return reply(200, `{"projects":[],"nextPageToken":"another"}`), nil
	})
	raw, err := m.gcpProjects(context.Background(), &record{Account: Account{Endpoint: gcpMCPEndpoint}}, "access", "next +/")
	if err != nil || !strings.Contains(string(raw), "another") {
		t.Fatal("discovery failed")
	}
}

func TestGCPResourceAndQuotaProjectsStayDistinct(t *testing.T) {
	args := []any{"gcloud", "compute", "instances", "list", "--project=resource-project", "--billing-project=other-quota"}
	command, _, err := gcpCommand(args, true, "mcp-project")
	if err != nil || strings.Contains(command, "mcp-project") || !strings.Contains(command, "other-quota") || !strings.Contains(command, "resource-project") {
		t.Fatalf("project boundary lost: %s %v", command, err)
	}
}
