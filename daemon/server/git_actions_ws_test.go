package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

func dialGitTestSocket(t *testing.T) *websocket.Conn {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "git-actions", "Test", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(host.Close)
	headers := http.Header{"Authorization": []string{calendarAuthHeader(private, manager.DaemonID(), "git-actions", "mewla-connect")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func gitTestRequest(t *testing.T, conn *websocket.Conn, request map[string]any) map[string]json.RawMessage {
	t.Helper()
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		var id string
		_ = json.Unmarshal(response["request_id"], &id)
		if id == request["request_id"] {
			return response
		}
	}
}

func TestGitActionsAuthenticatedWebSocket(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "first")
	writeGitDiffTestFile(t, repo, "a.txt", "two\n")
	conn := dialGitTestSocket(t)

	// Mutation: stage, then commit; both answer git_action with a result.
	response := gitTestRequest(t, conn, map[string]any{"type": "git_stage", "request_id": "r1", "cwd": repo, "repo_root": repo, "paths": []string{"a.txt"}})
	var kind, action string
	_ = json.Unmarshal(response["type"], &kind)
	_ = json.Unmarshal(response["action"], &action)
	if kind != "git_action" || action != "stage" {
		t.Fatalf("stage response = %v", response)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("staged = %q", got)
	}
	response = gitTestRequest(t, conn, map[string]any{"type": "git_commit", "request_id": "r2", "cwd": repo, "message": "Second"})
	var committed struct {
		Subject string `json:"subject"`
		SHA     string `json:"sha"`
	}
	_ = json.Unmarshal(response["result"], &committed)
	if committed.Subject != "Second" || committed.SHA == "" {
		t.Fatalf("commit response = %v", response)
	}

	// Read: the log has both commits.
	response = gitTestRequest(t, conn, map[string]any{"type": "git_log", "request_id": "r3", "cwd": repo, "limit": 10})
	var log gitLogPayload
	_ = json.Unmarshal(response["log"], &log)
	if len(log.Commits) != 2 || log.Commits[0].Subject != "Second" {
		t.Fatalf("log response = %s", response["log"])
	}

	// A failure is an error frame with a plain message and a request id.
	response = gitTestRequest(t, conn, map[string]any{"type": "git_commit", "request_id": "r4", "cwd": repo, "message": "again"})
	var code, message string
	_ = json.Unmarshal(response["code"], &code)
	_ = json.Unmarshal(response["message"], &message)
	if code != "git_commit_failed" || message != "Nothing is staged." {
		t.Fatalf("error frame = %v", response)
	}

	// A terminal that moved to another repository is refused before any change.
	other := newActionRepo(t)
	writeGitDiffTestFile(t, other, "b.txt", "b\n")
	response = gitTestRequest(t, conn, map[string]any{"type": "git_stage", "request_id": "r5", "cwd": other, "repo_root": repo, "paths": []string{"b.txt"}})
	_ = json.Unmarshal(response["code"], &code)
	_ = json.Unmarshal(response["message"], &message)
	if code != "git_stage_failed" || !strings.HasPrefix(message, "This terminal is no longer in ") {
		t.Fatalf("guard frame = %v", response)
	}
	if got := runGitDiffTestGit(t, other, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("guarded request staged %q", got)
	}
}
