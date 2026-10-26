package friendsvc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubWebReadsOnlyThePublicProfile(t *testing.T) {
	var tokenForm, profileAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		tokenForm = r.Form.Encode()
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		profileAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "octo", "created_at": "2020-01-02T03:04:05Z", "email": "x@example.com"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	g := &GitHubWeb{ClientID: "cid", ClientSecret: "sec", BaseURL: server.URL, APIURL: server.URL}

	identity, err := g.Exchange(context.Background(), "code1", "https://svc.example/cb")
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != "42" || identity.Login != "octo" || identity.CreatedAt.Year() != 2020 {
		t.Fatalf("identity = %+v", identity)
	}
	if profileAuth != "Bearer tok" || !strings.Contains(tokenForm, "code=code1") {
		t.Fatalf("calls = %q %q", tokenForm, profileAuth)
	}
	authorize := g.AuthorizeURL("st", "https://svc.example/cb", nil)
	if !strings.HasPrefix(authorize, server.URL+"/login/oauth/authorize?") || strings.Contains(authorize, "scope=") || strings.Contains(authorize, "sec") {
		t.Fatalf("authorize url = %q", authorize)
	}
}

func TestGitHubWebRefusesBadCodeAndShortProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
	}))
	defer server.Close()
	g := &GitHubWeb{BaseURL: server.URL, APIURL: server.URL}
	if _, err := g.Exchange(context.Background(), "x", "cb"); err == nil || strings.Contains(err.Error(), "x@") {
		t.Fatalf("bad code accepted: %v", err)
	}
}
