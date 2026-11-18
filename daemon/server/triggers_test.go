package server

import (
	"context"
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
	"github.com/daoleno/mewla/daemon/trigger"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

type wireSink struct{ n int }

func (s *wireSink) Append(trigger.Event) (trigger.Recorded, error) {
	s.n++
	return trigger.Recorded{ID: "ev-" + string(rune('0'+s.n)), Actionable: true}, nil
}
func (s *wireSink) EventHandled(string, string) (bool, bool, error) { return false, true, nil }

func TestTriggersOverTheWebSocket(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-triggers", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	store, err := trigger.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := trigger.NewEngine(store, &wireSink{}, func(context.Context, trigger.Trigger) trigger.Result {
		return trigger.Result{Stdout: "NEW perpetuo#1@aaaa"}
	})
	item, err := store.Create(trigger.Trigger{Title: "PR loop", Check: "./check.sh", IntervalSeconds: 600, WorkID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.SetTriggers(engine)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.broadcastEvents(ctx)
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-triggers", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	readType := func(want string) map[string]any {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				t.Fatalf("waiting for %s: %v", want, readErr)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["type"] == want {
				return payload
			}
		}
	}
	snapshot := readType("triggers_snapshot")
	list, _ := snapshot["triggers"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != item.ID || list[0].(map[string]any)["state"] != "watching" {
		t.Fatalf("snapshot %v", snapshot)
	}
	send := func(frame map[string]any) {
		t.Helper()
		if err := conn.WriteJSON(frame); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"type": "check_trigger", "request_id": "r1", "id": item.ID})
	// The reply and the broadcast race: either may come first.
	seen := map[string]map[string]any{}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for seen["trigger_updated"] == nil || seen["trigger_changed"] == nil {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			t.Fatalf("waiting for the check's reply and broadcast (have %v): %v", seen, readErr)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if kind, _ := payload["type"].(string); kind == "trigger_updated" || kind == "trigger_changed" {
			seen[kind] = payload
		}
	}
	updated := seen["trigger_updated"]
	view := updated["trigger"].(map[string]any)
	if updated["request_id"] != "r1" || view["last_fired_at"] == nil || view["pending"] != nil || view["last_output_hash"] != nil {
		t.Fatalf("check reply %v", updated)
	}
	if changed := seen["trigger_changed"]; changed["trigger"].(map[string]any)["id"] != item.ID {
		t.Fatalf("broadcast %v", changed)
	}
	send(map[string]any{"type": "pause_trigger", "request_id": "r2", "id": item.ID})
	if paused := readType("trigger_updated"); paused["trigger"].(map[string]any)["state"] != "paused" {
		t.Fatalf("pause reply %v", paused)
	}
	send(map[string]any{"type": "pause_trigger", "request_id": "r3", "id": "missing"})
	if failed := readType("error"); failed["code"] != "trigger_not_found" {
		t.Fatalf("missing trigger %v", failed)
	}
	send(map[string]any{"type": "delete_trigger", "request_id": "r4", "id": item.ID})
	if removed := readType("trigger_removed"); removed["trigger_id"] != item.ID {
		t.Fatalf("delete %v", removed)
	}
}
