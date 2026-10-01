package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/daoleno/zen/daemon/connections"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// Only used by the opt-in owned native runtime, never linked into production.
// adb reverse exposes this loopback provider to the owned emulator, allowing
// real system-browser consent/return without external OAuth app registration.
func newPluginsBrowserFixture(t *testing.T, manager *connections.Manager, root string) *httptest.Server {
	t.Helper()
	protocol := mcp.NewServer(&mcp.Implementation{Name: "Owned browser fixture", Version: "1"}, nil)
	protocol.AddTool(&mcp.Tool{Name: "read_browser_fixture", Description: "Read the owned OAuth fixture", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Native browser OAuth reached the real local MCP adapter"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return protocol }, nil)
	var base string
	var mu sync.Mutex
	codes := map[string]string{}
	counts := map[string]int{}
	event := func(kind string) {
		mu.Lock()
		defer mu.Unlock()
		counts[kind]++
		raw, _ := json.Marshal(counts)
		_ = os.WriteFile(filepath.Join(root, "browser-evidence.json"), raw, 0600)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "registration_endpoint": base + "/register", "revocation_endpoint": base + "/revoke", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"read"}})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		event("registration")
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "owned-browser-client", "token_endpoint_auth_method": "none"})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Form.Get("redirect_uri") != base+"/plugins/oauth/callback" || r.Form.Get("state") == "" || r.Form.Get("code_challenge_method") != "S256" {
			http.Error(w, "Invalid fixture authorization", 400)
			return
		}
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			page := `<!doctype html><meta name="viewport" content="width=device-width"><title>Owned OAuth fixture</title><h1>Authorize Zen</h1><p>Local test provider. Grants access to one read-only fixture tool.</p><form method="POST" action="/authorize">`
			for _, key := range []string{"redirect_uri", "state", "code_challenge", "code_challenge_method"} {
				page += `<input type="hidden" name="` + key + `" value="` + html.EscapeString(r.Form.Get(key)) + `">`
			}
			_, _ = w.Write([]byte(page + `<button style="font-size:20px;padding:16px">Authorize fixture</button></form>`))
			return
		}
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		var b [24]byte
		_, _ = rand.Read(b[:])
		code := base64.RawURLEncoding.EncodeToString(b[:])
		mu.Lock()
		codes[code] = r.Form.Get("code_challenge")
		mu.Unlock()
		event("browser_consent")
		http.Redirect(w, r, base+"/plugins/oauth/callback?"+url.Values{"code": {code}, "state": {r.Form.Get("state")}, "iss": {base}}.Encode(), http.StatusSeeOther)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if resources := r.PostForm["resource"]; len(resources) != 1 || resources[0] != base+"/mcp" {
			http.Error(w, `{"error":"invalid_target"}`, 400)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			mu.Lock()
			challenge, ok := codes[r.Form.Get("code")]
			delete(codes, r.Form.Get("code"))
			mu.Unlock()
			if !ok || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			event("pkce_exchange")
		case "refresh_token":
			if r.Form.Get("refresh_token") != "owned-fixture-refresh" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			event("refresh")
		default:
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "owned-fixture-access", "refresh_token": "owned-fixture-refresh", "token_type": "Bearer", "expires_in": 30, "scope": "read"})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) { event("revoke") })
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer owned-fixture-access" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "POST" {
			event("mcp_post")
		}
		handler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/plugins/oauth/callback", manager.OAuthCallback)
	fixture := httptest.NewServer(mux)
	base = fixture.URL
	if err := os.WriteFile(filepath.Join(root, "browser-endpoint"), []byte(base), 0600); err != nil {
		fixture.Close()
		t.Fatal(err)
	}
	return fixture
}
