package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/gorilla/websocket"
)

func TestRegisterPushTellsTheDeviceHookTheTimeZoneAndLocale(t *testing.T) {
	authManager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := authManager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := authManager.EnrollDevice(pairing.Value, authManager.DaemonID(), authManager.PublicKeyHex(), "device-locale", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	srv := New(authManager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	pusher := &recordingNotificationPusher{}
	srv.pusher = pusher
	heard := make(chan [2]string, 1)
	srv.SetDeviceHook(func(timeZone, locale string) { heard <- [2]string{timeZone, locale} })
	httpServer := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer httpServer.Close()
	header := http.Header{}
	header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-locale", "mewla-connect"))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "register_push", "push_token": "token", "server_ref": "ref", "time_zone": " Asia/Shanghai ", "locale": "zh-Hans-CN"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-heard:
		if got != [2]string{"Asia/Shanghai", "zh-Hans-CN"} {
			t.Fatalf("hook heard %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the device hook was not called")
	}
	// The push token is kept under the paired device that sent it, and a
	// revoked device is forgotten.
	calls := pusher.snapshot()
	if len(calls) != 1 || calls[0].kind != "registration" || calls[0].deviceID != "device-locale" || calls[0].serverRef != "ref" {
		t.Fatalf("registration calls = %+v", calls)
	}
	srv.revokeAuthenticatedDevice("device-locale")
	calls = pusher.snapshot()
	if len(calls) != 2 || calls[1].kind != "forget" || calls[1].deviceID != "device-locale" {
		t.Fatalf("revocation calls = %+v", calls)
	}
}
