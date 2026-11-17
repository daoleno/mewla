package modelprofiles

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Defect regression (11-18): an upstream whose connection drops mid-stream
// (an ssh tunnel to a Claude proxy closing) must reach the client as a broken
// response. Ending the chunked response cleanly hands Claude Code a truncated
// zstd frame that looks complete ("ZstdDecompressionError"), or a plain SSE
// stream that silently stops.
func TestRouteAndGatewayAbortClientOnUpstreamBreak(t *testing.T) {
	var zbuf bytes.Buffer
	zw, err := zstd.NewWriter(&zbuf)
	if err != nil {
		t.Fatal(err)
	}
	sse := bytes.Repeat([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"), 200)
	if _, err := zw.Write(sse); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	compressed := zbuf.Bytes()

	for _, tc := range []struct {
		name     string
		encoding string
		payload  []byte
	}{
		{"zstd", "zstd", compressed[:len(compressed)/2]},
		{"plain", "", sse[:len(sse)/2]},
	} {
		upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			if tc.encoding != "" {
				w.Header().Set("Content-Encoding", tc.encoding)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tc.payload)
			w.(http.Flusher).Flush()
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
		})

		t.Run("route/"+tc.name, func(t *testing.T) {
			table := NewRouteTable()
			profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
			state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			srv := newFakeUpstream(t, NewRouter(table).ServeHTTP)
			root, err := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
			if err != nil {
				t.Fatal(err)
			}
			assertBrokenStream(t, root+"/v1/messages", tc.encoding, len(tc.payload))
		})

		t.Run("gateway/"+tc.name, func(t *testing.T) {
			g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
			g.SetUpstream(gatewayUpstreamFor(upstream.URL))
			srv := newFakeUpstream(t, g.ServeHTTP)
			assertBrokenStream(t, srv.URL+"/v1/responses", tc.encoding, len(tc.payload))
		})
	}
}

// assertBrokenStream posts like Claude Code (it asks for zstd and decodes
// itself) and requires the body read to fail rather than end cleanly.
func assertBrokenStream(t *testing.T, url, encoding string, sent int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"model":"claude-sonnet-4-6","stream":true,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != encoding {
		t.Fatalf("Content-Encoding=%q, want %q", got, encoding)
	}
	got, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Fatalf("upstream broke after %d bytes but the client saw a clean end after %d bytes", sent, len(got))
	}
	if !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Logf("client read error: %v", readErr)
	}
	if len(got) != sent {
		t.Fatalf("client got %d bytes before the break, upstream sent %d", len(got), sent)
	}
}
