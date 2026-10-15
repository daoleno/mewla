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

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/auth"
)

func TestAgentUseSwitchIsSignedOverItsBody(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	service, err := agentuse.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	srv.SetAgentUse(service)
	header := func(purpose string) string {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + manager.DaemonID() + ":" + timestamp + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(signature)
	}
	do := func(method, purpose string, body []byte) (int, agentuse.Status) {
		request := httptest.NewRequest(method, "/agent-use", bytes.NewReader(body))
		request.Header.Set("Authorization", header(purpose))
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		var status agentuse.Status
		_ = json.Unmarshal(response.Body.Bytes(), &status)
		return response.Code, status
	}
	postPurpose := func(body []byte) string {
		sum := sha256.Sum256(body)
		return "mewla-agent-use:POST:/agent-use:sha256=" + hex.EncodeToString(sum[:])
	}

	if code, status := do(http.MethodGet, "mewla-agent-use:GET:/agent-use", nil); code != http.StatusOK || !status.Enabled {
		t.Fatalf("on by default: %d %+v", code, status)
	}
	if code, _ := do(http.MethodGet, "mewla-screen", nil); code != http.StatusUnauthorized {
		t.Fatalf("another purpose read the status: %d", code)
	}
	off := []byte(`{"enabled":false}`)
	// A signature over one body cannot carry another.
	if code, _ := do(http.MethodPost, postPurpose([]byte(`{"enabled":true}`)), off); code != http.StatusUnauthorized {
		t.Fatalf("body swapped under a signature: %d", code)
	}
	if code, status := do(http.MethodPost, postPurpose(off), off); code != http.StatusOK || status.Enabled || status.Browser.State != agentuse.BrowserOff {
		t.Fatalf("turn off: %d %+v", code, status)
	}
	if code, status := do(http.MethodGet, "mewla-agent-use:GET:/agent-use", nil); code != http.StatusOK || status.Enabled {
		t.Fatalf("stays off: %d %+v", code, status)
	}
	bad := []byte(`{"key":"not a key"}`)
	if code, _ := do(http.MethodPost, postPurpose(bad), bad); code != http.StatusBadRequest {
		t.Fatalf("malformed key accepted: %d", code)
	}
	two := []byte(`{"enabled":true,"fix":"retry"}`)
	if code, _ := do(http.MethodPost, postPurpose(two), two); code != http.StatusBadRequest {
		t.Fatalf("two changes at once accepted: %d", code)
	}
}
