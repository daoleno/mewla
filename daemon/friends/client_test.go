package friends

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/reqsign"
)

// The client signs the path the server sees, mount prefix included.
func TestClientSignsTheMountedPath(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier := &reqsign.Verifier{Scheme: reqsign.FriendsAgentScheme, Verify: auth.VerifyLinkSignature, KeyBytes: 32}
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, rejection := verifier.Authenticate(w, r)
		if rejection != nil {
			http.Error(w, rejection.Message, rejection.Status)
			return
		}
		got = r.URL.Path + " " + result.Key[:8]
		_, _ = w.Write([]byte(`{"handle":"h","replaced":true}`))
	}))
	defer server.Close()
	client := &Client{BaseURL: server.URL + "/friends", Auth: manager}
	result, err := client.Link(context.Background(), "00112233445566778899aabbccddeeff", "Mochi")
	if err != nil || result.Handle != "h" || !result.Replaced {
		t.Fatalf("link = %+v, %v", result, err)
	}
	if want := "/friends/v1/agent/link " + manager.PublicKeyHex()[:8]; got != want {
		t.Fatalf("server saw %q, want %q", got, want)
	}
	// A refusal surfaces its code.
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_code"}`, http.StatusBadRequest)
	}))
	defer denied.Close()
	_, err = (&Client{BaseURL: denied.URL, Auth: manager}).Link(context.Background(), "x", "")
	if serviceError, ok := err.(*ServiceError); !ok || serviceError.Code != "invalid_code" {
		t.Fatalf("error = %v", err)
	}
}
