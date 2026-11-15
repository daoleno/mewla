package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/auth"
)

func TestClientAppFromRequestReadsTheAppSubprotocol(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		protocols []string
		want      auth.ClientApp
	}{
		{"an app that names itself", "", []string{"mewla.auth.x", "mewla.sessions.tailless", "mewla.app.android.0.4.6"}, auth.ClientApp{AppVersion: "0.4.6", AppPlatform: "android"}},
		{"a beta on iOS", "", []string{"mewla.auth.x", "mewla.app.ios.0.4.7-beta.1"}, auth.ClientApp{AppVersion: "0.4.7-beta.1", AppPlatform: "ios"}},
		{"an NSH app before it said (0.4.0 to 0.4.5)", "", []string{"mewla.auth.x", "mewla.sessions.tailless"}, auth.ClientApp{}},
		{"the Expo app: a header and no subprotocol", "Mewla-Device x", nil, auth.ClientApp{AppLegacy: true}},
		{"an unknown platform is not kept", "", []string{"mewla.auth.x", "mewla.app.toaster.1.0.0"}, auth.ClientApp{}},
		{"a version that is not one is not kept", "", []string{"mewla.auth.x", "mewla.app.web.latest"}, auth.ClientApp{}},
		{"an overlong version is not kept", "", []string{"mewla.auth.x", "mewla.app.web.1" + strings.Repeat("0", 40)}, auth.ClientApp{}},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		if len(c.protocols) > 0 {
			r.Header.Set("Sec-Websocket-Protocol", strings.Join(c.protocols, ", "))
		}
		if got := clientAppFromRequest(r); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A paired device's app connection leaves its app version on the device, and
// the next connection replaces it: what `mewla devices list` shows.
func TestTheAppConnectionKeepsTheDevicesAppVersion(t *testing.T) {
	srv := New(nil, nil, nil, nil, nil, nil, nil)
	defer srv.shutdownAuthenticatedClients()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.auth = manager
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "phone", "Phone", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	t.Cleanup(host.Close)
	url := "ws" + strings.TrimPrefix(host.URL, "http")

	appOf := func() auth.ClientApp {
		for _, device := range manager.ListDevices() {
			if device.ID == "phone" {
				return device.ClientApp
			}
		}
		t.Fatal("the device is gone")
		return auth.ClientApp{}
	}
	connect := func(header http.Header, protocols ...string) {
		t.Helper()
		conn, _, err := (&websocket.Dialer{Subprotocols: protocols}).Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
	signed := func() string {
		return calendarAuthHeader(private, manager.DaemonID(), "phone", "mewla-connect")
	}
	viaSubprotocol := func() string {
		return authSubprotocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(signed()))
	}

	connect(http.Header{"Authorization": {signed()}})
	if got := appOf(); got != (auth.ClientApp{AppLegacy: true}) {
		t.Fatalf("the Expo app's sign-in: %+v", got)
	}
	connect(nil, viaSubprotocol(), "mewla.sessions.tailless", "mewla.app.android.0.4.6")
	if got := appOf(); got != (auth.ClientApp{AppVersion: "0.4.6", AppPlatform: "android"}) {
		t.Fatalf("after the new app connected: %+v", got)
	}
	reloaded, err := auth.NewManager(manager.StorageDir())
	if err != nil {
		t.Fatal(err)
	}
	if devices := reloaded.ListDevices(); len(devices) != 1 || devices[0].AppVersion != "0.4.6" || devices[0].AppPlatform != "android" {
		t.Fatalf("not kept across a restart: %+v", devices)
	}
	connect(nil, viaSubprotocol())
	if got := appOf(); got != (auth.ClientApp{}) {
		t.Fatalf("an app that says nothing replaces what an earlier one said: %+v", got)
	}
}
