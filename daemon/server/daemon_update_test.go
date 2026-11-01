package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

type fakeDaemonUpdater struct {
	status    DaemonUpdateStatus
	installed int
	err       error
}

func (f *fakeDaemonUpdater) Status() DaemonUpdateStatus { return f.status }
func (f *fakeDaemonUpdater) Install() error {
	f.installed++
	if f.err == nil {
		f.status.Updating = true
	}
	return f.err
}

func TestDaemonUpdateReportsVersionAndInstallsOnlyForASignedPhone(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	updater := &fakeDaemonUpdater{status: DaemonUpdateStatus{Version: "0.2.88", Supported: true, Latest: "0.2.89"}}
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	srv.SetVersion("0.2.88")
	srv.SetDaemonUpdater(updater)
	header := func(purpose string) string {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + manager.DaemonID() + ":" + timestamp + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(signature)
	}
	do := func(method, purpose string) (int, DaemonUpdateStatus) {
		request := httptest.NewRequest(method, "/daemon-update", nil)
		if purpose != "" {
			request.Header.Set("Authorization", header(purpose))
		}
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		var status DaemonUpdateStatus
		_ = json.Unmarshal(response.Body.Bytes(), &status)
		return response.Code, status
	}

	if code, _ := do(http.MethodGet, ""); code != http.StatusUnauthorized {
		t.Fatalf("unsigned status read: %d", code)
	}
	if code, _ := do(http.MethodPost, ""); code != http.StatusUnauthorized || updater.installed != 0 {
		t.Fatalf("unsigned install: %d (installs %d)", code, updater.installed)
	}
	// A signature for reading cannot start an install.
	if code, _ := do(http.MethodPost, "mewla-daemon-update:GET:/daemon-update"); code != http.StatusUnauthorized || updater.installed != 0 {
		t.Fatalf("read signature started an install: %d (installs %d)", code, updater.installed)
	}
	if code, status := do(http.MethodGet, "mewla-daemon-update:GET:/daemon-update"); code != http.StatusOK || status.Version != "0.2.88" || status.Latest != "0.2.89" || !status.Supported {
		t.Fatalf("status: %d %+v", code, status)
	}
	if code, status := do(http.MethodPost, "mewla-daemon-update:POST:/daemon-update"); code != http.StatusOK || !status.Updating || updater.installed != 1 {
		t.Fatalf("install: %d %+v (installs %d)", code, status, updater.installed)
	}
	updater.err = ErrUpdateUnavailable
	if code, _ := do(http.MethodPost, "mewla-daemon-update:POST:/daemon-update"); code != http.StatusConflict {
		t.Fatalf("nothing to install: %d", code)
	}
}

func TestHealthReportsTheVersionTheUpdateGateChecks(t *testing.T) {
	manager, _, _ := sessionFileAuthFixture(t)
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	srv.SetVersion("0.2.88")
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	var payload struct {
		DaemonID string `json:"daemon_id"`
		Version  string `json:"version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != http.StatusOK {
		t.Fatalf("health: %d %v %s", response.Code, err, response.Body.String())
	}
	if payload.Version != "0.2.88" || payload.DaemonID != manager.DaemonID() {
		t.Fatalf("health = %+v", payload)
	}
}
