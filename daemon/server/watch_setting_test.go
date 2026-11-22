package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

type fakeWatchSwitch struct{ on bool }

func (f *fakeWatchSwitch) Enabled() bool { return f.on }
func (f *fakeWatchSwitch) SetEnabled(enabled bool) (bool, error) {
	f.on = enabled
	return f.on, nil
}

func TestWatchSwitchReadsAndSetsSignedOverItsBody(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	header := func(purpose string) string {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + manager.DaemonID() + ":" + timestamp + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(signature)
	}
	do := func(method, purpose string, body []byte) (int, watchSetting) {
		request := httptest.NewRequest(method, "/watch", bytes.NewReader(body))
		request.Header.Set("Authorization", header(purpose))
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		var setting watchSetting
		_ = json.Unmarshal(response.Body.Bytes(), &setting)
		return response.Code, setting
	}
	postPurpose := func(body []byte) string {
		sum := sha256.Sum256(body)
		return "mewla-watch:POST:/watch:sha256=" + hex.EncodeToString(sum[:])
	}

	// No Watch on this computer: unavailable, not a dead switch.
	if code, _ := do(http.MethodGet, "mewla-watch:GET:/watch", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("without Watch: %d", code)
	}
	watch := &fakeWatchSwitch{}
	srv.SetWatch(watch)
	if code, setting := do(http.MethodGet, "mewla-watch:GET:/watch", nil); code != http.StatusOK || setting.Enabled {
		t.Fatalf("off: %d %+v", code, setting)
	}
	if code, _ := do(http.MethodGet, "mewla-agent-use:GET:/agent-use", nil); code != http.StatusUnauthorized {
		t.Fatalf("another purpose read the switch: %d", code)
	}
	on := []byte(`{"enabled":true}`)
	if code, _ := do(http.MethodPost, postPurpose([]byte(`{"enabled":false}`)), on); code != http.StatusUnauthorized || watch.on {
		t.Fatalf("body swapped under a signature: %d on=%v", code, watch.on)
	}
	if code, setting := do(http.MethodPost, postPurpose(on), on); code != http.StatusOK || !setting.Enabled || !watch.on {
		t.Fatalf("turn on: %d %+v", code, setting)
	}
	empty := []byte(`{}`)
	if code, _ := do(http.MethodPost, postPurpose(empty), empty); code != http.StatusBadRequest {
		t.Fatalf("a change without enabled accepted: %d", code)
	}
	if code, setting := do(http.MethodGet, "mewla-watch:GET:/watch", nil); code != http.StatusOK || !setting.Enabled {
		t.Fatalf("stays on: %d %+v", code, setting)
	}
}
