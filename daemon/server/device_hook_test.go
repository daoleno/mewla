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
	srv.pusher = &recordingNotificationPusher{}
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
}
