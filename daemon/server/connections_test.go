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

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/connections"
	"github.com/gorilla/websocket"
)

func TestPluginsRequirePairedDeviceAndPreserveRequestIdentity(t *testing.T) {
	a, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := connections.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(a, nil, nil, nil, nil, nil, nil)
	s.SetConnections(m)
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	target := "ws" + strings.TrimPrefix(h.URL, "http") + "/ws"
	if c, _, err := websocket.DefaultDialer.Dial(target, nil); err == nil {
		c.Close()
		t.Fatal("unpaired caller reached plugins")
	}
	token, _ := a.IssuePairingToken(time.Minute)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	_, err = a.EnrollDevice(token.Value, a.DaemonID(), a.PublicKeyHex(), "plugin-phone", "phone", hex.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{}
	headers.Set("Authorization", calendarAuthHeader(key, a.DaemonID(), "plugin-phone", "zen-connect"))
	c, _, err := websocket.DefaultDialer.Dial(target, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.WriteJSON(clientMessage{Type: "connections", RequestID: "catalog-1", ConnectionRequest: &connections.Request{Action: "list"}}); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]json.RawMessage
		json.Unmarshal(raw, &result)
		if string(result["type"]) != `"connections_result"` {
			continue
		}
		if string(result["request_id"]) != `"catalog-1"` {
			t.Fatal("request identity lost")
		}
		var resultBody connections.Response
		if json.Unmarshal(result["connections"], &resultBody) != nil || len(resultBody.Catalog) != 7 {
			t.Fatal("missing plugin catalog")
		}
		break
	}
	if _, err = a.RevokeDevice("plugin-phone"); err != nil {
		t.Fatal(err)
	}
	// Revocation closes or rejects the existing socket; it cannot keep reading
	// account data or invoke tools using the previously authenticated transport.
	_ = c.WriteJSON(clientMessage{Type: "connections", RequestID: "after-revoke", ConnectionRequest: &connections.Request{Action: "list"}})
	if _, _, err = c.ReadMessage(); err == nil {
		t.Fatal("revoked paired device retained access")
	}
}
