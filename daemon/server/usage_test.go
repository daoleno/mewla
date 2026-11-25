package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/usage"
)

func usageOnByDefault(t *testing.T) {
	t.Helper()
	for _, key := range []string{usage.OffEnv, "DO_NOT_TRACK", "CI", usage.EndpointEnv} {
		t.Setenv(key, "")
	}
	usage.Release = true
	t.Cleanup(func() { usage.Release = false })
}

func TestUsageSwitchAndAppDailyAreSignedOverTheirBody(t *testing.T) {
	usageOnByDefault(t)
	a, priv, device := sessionFileAuthFixture(t)
	s := New(a, nil, nil, nil, nil, nil, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	request := func(method, purpose, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/usage", strings.NewReader(body))
		req.Header.Set("Authorization", calendarAuthHeader(priv, a.DaemonID(), device, purpose))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var view map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&view)
		return resp.StatusCode, view
	}
	post := func(body string) (int, map[string]any) {
		sum := sha256.Sum256([]byte(body))
		return request(http.MethodPost, usagePostPurpose+":sha256="+hex.EncodeToString(sum[:]), body)
	}
	if code, view := request(http.MethodGet, usageGetPurpose, ""); code != http.StatusOK || view["enabled"] != true {
		t.Fatalf("GET: %d %v", code, view)
	}
	sum := sha256.Sum256([]byte(`{"action":"set","enabled":true}`))
	if code, _ := request(http.MethodPost, usagePostPurpose+":sha256="+hex.EncodeToString(sum[:]), `{"action":"set","enabled":false}`); code != http.StatusUnauthorized {
		t.Fatalf("a body other than the signed one: %d", code)
	}
	if code, view := post(`{"action":"set","enabled":false}`); code != http.StatusOK || view["enabled"] != false || view["locked"] != "" {
		t.Fatalf("off: %d %v", code, view)
	}
	if settings := usage.ReadSettings(a.StorageDir()); settings.Enabled == nil || *settings.Enabled {
		t.Fatalf("settings = %+v", settings)
	}
	if code, view := post(`{"action":"set","enabled":true}`); code != http.StatusOK || view["enabled"] != true {
		t.Fatalf("on: %d %v", code, view)
	}
	app := strings.Repeat("ab", 16)
	if code, _ := post(`{"action":"app_daily","app":"` + app + `","platform":"ios","os_major":"18","version":"0.5.0"}`); code != http.StatusOK {
		t.Fatalf("app_daily: %d", code)
	}
	if code, _ := post(`{"action":"app_daily","app":"` + app + `","platform":"ios","os_major":"18","version":"/Users/alice"}`); code != http.StatusBadRequest {
		t.Fatalf("app_daily with a path: %d", code)
	}
	spool, _ := os.ReadFile(filepath.Join(a.StorageDir(), usage.SpoolFile))
	if strings.Count(string(spool), "app_daily") != 1 || strings.Contains(string(spool), "alice") {
		t.Fatalf("spool = %s", spool)
	}
	// An environment switch locks counts off whatever the app says.
	t.Setenv(usage.OffEnv, "0")
	if code, view := request(http.MethodGet, usageGetPurpose, ""); code != http.StatusOK || view["enabled"] != false || view["locked"] != usage.OffByEnv {
		t.Fatalf("locked: %d %v", code, view)
	}
}

func TestPairFailureClasses(t *testing.T) {
	for err, want := range map[error]string{
		auth.ErrExpiredPairingToken: "expired",
		auth.ErrInvalidPairingToken: "admission",
		auth.ErrWrongDaemon:         "pin",
		auth.ErrDeviceKeyMismatch:   "pin",
		errors.New("disk full"):     "other",
	} {
		if got := pairFailureClass(err); got != want {
			t.Errorf("%v = %s, want %s", err, got, want)
		}
	}
	for in, want := range map[string]string{"desktop": "mac", "android": "android", "ios": "ios", "toaster": "other"} {
		if usagePlatform(in) != want {
			t.Errorf("platform %s = %s", in, usagePlatform(in))
		}
	}
}
