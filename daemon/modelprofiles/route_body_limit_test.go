package modelprofiles

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 11-19: KidsScoreLook stalled on "Request too large (max 32MB) … Run
// /compact" with a conversation between 8 and 32 MB; the 413 was the route's
// own 8 MiB cap. A body the provider accepts must reach it.
func TestRouteForwardsBodiesUpToTheProvidersOwnLimit(t *testing.T) {
	var got int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = len(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","role":"assistant","content":[]}`)
	}))
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	state, err := table.BindLaunch("big", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(table).Handler())
	defer srv.Close()
	root, err := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	post := func(size int) int {
		image := strings.Repeat("A", size)
		body := `{"model":"claude-sonnet-4-6","max_tokens":1,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}}]}]}`
		request, _ := http.NewRequest(http.MethodPost, root+"/v1/messages", bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := post(20 << 20); status != http.StatusOK || got < 20<<20 {
		t.Fatalf("a 20 MB conversation: status %d, upstream got %d bytes", status, got)
	}
	if status := post(MaxRouteRequestBodyBytes + 1); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("over the route's own budget: status %d", status)
	}
}
