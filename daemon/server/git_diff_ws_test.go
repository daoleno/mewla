package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitDiffPatchAuthenticatedWebSocket(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "tracked.ts", "old\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "baseline")
	writeGitDiffTestFile(t, repo, "tracked.ts", "new staged\n")
	runGitDiffTestGit(t, repo, "add", "tracked.ts")
	writeGitDiffTestFile(t, repo, "tracked.ts", "new working\n")
	writeGitDiffTestFile(t, repo, " odd\tfile ", "new\n")
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "diff-test", "Test", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer host.Close()
	url := "ws" + strings.TrimPrefix(host.URL, "http")
	if conn, _, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		conn.Close()
		t.Fatal("unauthenticated diff socket accepted")
	}
	headers := http.Header{"Authorization": []string{calendarAuthHeader(private, manager.DaemonID(), "diff-test", "mewla-connect")}}
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// " odd\tfile " is untracked; tracked.ts has a staged and an unstaged change.
	for path, scopes := range map[string][]string{" odd\tfile ": {"untracked"}, "tracked.ts": {"staged", "unstaged"}} {
		if err = conn.WriteJSON(map[string]any{"type": "git_diff_patch", "request_id": "patch-test", "cwd": repo, "path": path}); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Type      string              `json:"type"`
				RequestID string              `json:"request_id"`
				Patch     gitDiffPatchPayload `json:"patch"`
			}
			if err = json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			if response.RequestID != "patch-test" {
				continue
			}
			if response.Type != "git_diff_patch" || response.Patch.Path != path || len(response.Patch.Sections) != len(scopes) {
				t.Fatalf("response for %q: %s", path, raw)
			}
			for index, scope := range scopes {
				if response.Patch.Sections[index].Scope != scope {
					t.Fatalf("%q section %d scope = %q, want %q", path, index, response.Patch.Sections[index].Scope, scope)
				}
			}
			break
		}
	}
}
