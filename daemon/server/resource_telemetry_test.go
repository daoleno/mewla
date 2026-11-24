package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/statedir"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

func TestResourceTelemetryAuthenticatedHTTPAndWebSocketContract(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "resource-device", "phone", hex.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	srv.resourceSampler.Sample(srv.watcher, time.Now())
	server := httptest.NewServer(srv.Handler())
	defer server.Close()
	resp, err := http.Get(server.URL + "/resources")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("unauthenticated resource snapshot exposed")
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/resources", nil)
	request.Header.Set("Authorization", calendarAuthHeader(priv, manager.DaemonID(), "resource-device", "mewla-resource-telemetry"))
	resp, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}
	var payload map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["version"] != float64(2) || payload["type"] != "resource_telemetry" {
		t.Fatal(payload)
	}
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(priv, manager.DaemonID(), "resource-device", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteJSON(map[string]any{"type": "get_resource_telemetry", "request_id": "r1"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if err = conn.ReadJSON(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["request_id"] != "r1" {
			continue
		}
		if payload["type"] != "resource_telemetry" || payload["version"] != float64(2) || payload["history"] == nil {
			t.Fatal(payload)
		}
		break
	}
}

func TestHoldsRideInTelemetryAndRestorePutsAnItemBack(t *testing.T) {
	root := t.TempDir()
	statedir.SetRoot(root)
	t.Cleanup(func() { statedir.SetRoot("") })
	loose := filepath.Join(root, "t", "loose")
	if err := os.MkdirAll(loose, 0o700); err != nil {
		t.Fatal(err)
	}
	item, err := gc.Trash(root, gc.KindUnmarked, loose, "", gc.ReasonUnmarked, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "holds-device", "phone", hex.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	w := watcher.New(time.Second)
	w.ConfigureDelegatedResources("0123456789abcdef0123456789abcdef")
	srv := New(manager, w, nil, nil, nil, nil, nil)
	srv.resourceSampler.Sample(srv.watcher, time.Now())
	server := httptest.NewServer(srv.Handler())
	defer server.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(priv, manager.DaemonID(), "holds-device", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reply := func(request map[string]any) map[string]any {
		t.Helper()
		if err := conn.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			var payload map[string]any
			if err := conn.ReadJSON(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["request_id"] == request["request_id"] {
				return payload
			}
		}
	}
	restorable := func(payload map[string]any) []any {
		holds, _ := payload["holds"].(map[string]any)
		today, _ := holds["collected_today"].(map[string]any)
		list, _ := today["restorable"].([]any)
		return list
	}
	telemetry := reply(map[string]any{"type": "get_resource_telemetry", "request_id": "t1"})
	if list := restorable(telemetry); len(list) != 1 || list[0].(map[string]any)["id"] != item.ID {
		t.Fatalf("holds %v", telemetry["holds"])
	}
	restored := reply(map[string]any{"type": "gc_restore", "request_id": "r1", "id": item.ID})
	if restored["type"] != "gc_restored" || restored["original"] != loose {
		t.Fatalf("restore %v", restored)
	}
	if _, err := os.Stat(loose); err != nil {
		t.Fatalf("not back: %v", err)
	}
	// The summary is not reused after a restore: the item is no longer offered.
	if list := restorable(reply(map[string]any{"type": "get_resource_telemetry", "request_id": "t2"})); len(list) != 0 {
		t.Fatalf("still restorable: %v", list)
	}
	if failed := reply(map[string]any{"type": "gc_restore", "request_id": "r2", "id": item.ID}); failed["type"] != "error" && failed["error"] == nil {
		t.Fatalf("a second restore of the same item: %v", failed)
	}
}
