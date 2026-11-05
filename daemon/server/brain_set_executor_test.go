package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// blockedReadyWatcher is an agent whose pane never becomes ready for input, the
// slowest a new Brain host can be.
type blockedReadyWatcher struct {
	killTrackingWatcher
	release chan struct{}
}

func (w *blockedReadyWatcher) SendInputWithReceiptWhenReadyResult(
	sessionID, command, text string,
	receiptFor watcher.InputReceiptForGeneration,
) (watcher.InputResult, watcher.OwnedGeneration, error) {
	<-w.release
	return w.killTrackingWatcher.SendInputWithReceiptWhenReadyResult(sessionID, command, text, receiptFor)
}

// Switching Brain's executor answers as soon as the choice is kept: not after
// the new host is up (the App gave up at 15 s while a slow agent was still
// opening), and a choice that cannot work is still refused with its reason.
func TestBrainSetExecutorAnswersWithoutWaitingForTheNewHost(t *testing.T) {
	restore := brain.SetHostLookPath(func(name string) (string, error) {
		if name == "definitely-not-installed-agent" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + name, nil
	})
	defer restore()
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "thread"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("mewla-worker-brain-old:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &blockedReadyWatcher{release: make(chan struct{})}
	defer close(fw.release)
	service := brain.NewService(store, fw, work.NewExecutorConfig(map[string]work.Executor{
		"codex": {Name: "codex", Command: "sh", Kind: "codex"},
		"other": {Name: "other", Command: "sh", Kind: "claude"},
		"ghost": {Name: "ghost", Command: "definitely-not-installed-agent", Kind: "claude"},
	}))

	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-switch", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, service)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-switch", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	request := func(executor string) map[string]any {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{"type": "brain_set_executor", "request_id": "r-" + executor, "executor_id": executor}); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("no answer for %s within 3s: %v", executor, err)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["request_id"] == "r-"+executor {
				return payload
			}
		}
	}

	started := time.Now()
	reply := request("other")
	if time.Since(started) > 2*time.Second {
		t.Fatalf("answer took %s", time.Since(started))
	}
	hostExecutor := reply["brain"].(map[string]any)["host_executor"].(map[string]any)
	if reply["type"] != "brain_snapshot" || hostExecutor["id"] != "other" {
		t.Fatalf("reply = %#v", reply)
	}
	if fw.created != 0 || len(fw.killed) != 0 {
		t.Fatalf("the request started the host itself: created=%d killed=%v", fw.created, fw.killed)
	}
	if len(srv.brainHostRecovery) != 1 {
		t.Fatal("the host continuity owner was not woken to start the new host")
	}

	refused := request("ghost")
	if refused["type"] != "error" || refused["code"] != "brain_set_executor_failed" || !strings.Contains(refused["message"].(string), "not installed") {
		t.Fatalf("an uninstalled executor was not refused with its reason: %#v", refused)
	}
	host, _ := store.HostSession()
	if host.ExecutorID != "other" {
		t.Fatalf("a refused choice replaced the kept one: %q", host.ExecutorID)
	}
}
