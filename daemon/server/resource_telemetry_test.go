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
