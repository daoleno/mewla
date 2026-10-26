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
	"github.com/daoleno/mewla/daemon/friends"
)

func TestFriendsLinkComesFromAPairedPhoneAndIsSignedOverItsBody(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	var linkedWith string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Code string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		linkedWith = r.URL.Path + " " + body.Code
		_, _ = w.Write([]byte(`{"handle":"owner","replaced":false}`))
	}))
	defer service.Close()
	t.Setenv("MEWLA_FRIENDS_URL", service.URL+"/friends")
	srv := New(manager, nil, nil, nil, nil, nil, nil)

	header := func(purpose string) string {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + manager.DaemonID() + ":" + timestamp + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(signature)
	}
	do := func(method, purpose string, body []byte) (int, friendsStatus) {
		request := httptest.NewRequest(method, "/friends", bytes.NewReader(body))
		if purpose != "" {
			request.Header.Set("Authorization", header(purpose))
		}
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		var status friendsStatus
		_ = json.Unmarshal(response.Body.Bytes(), &status)
		return response.Code, status
	}
	post := func(body []byte) string {
		sum := sha256.Sum256(body)
		return "mewla-friends:POST:/friends:sha256=" + hex.EncodeToString(sum[:])
	}

	if code, _ := do(http.MethodGet, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unsigned read: %d", code)
	}
	if code, status := do(http.MethodGet, "mewla-friends:GET:/friends", nil); code != http.StatusOK || status.Linked {
		t.Fatalf("starts off: %d %+v", code, status)
	}
	link := []byte(`{"link_code":"00112233445566778899aabbccddeeff","name":"Mochi"}`)
	if code, _ := do(http.MethodPost, post([]byte(`{"link_code":"ffeeddccbbaa99887766554433221100"}`)), link); code != http.StatusUnauthorized {
		t.Fatalf("body swapped under a signature: %d", code)
	}
	if code, status := do(http.MethodPost, post(link), link); code != http.StatusOK || !status.Linked || status.Handle != "owner" {
		t.Fatalf("link: %d %+v", code, status)
	}
	if linkedWith != "/friends/v1/agent/link 00112233445566778899aabbccddeeff" {
		t.Fatalf("service saw %q", linkedWith)
	}
	if _, err := friends.NewStore(manager.StorageDir()).Link(); err != nil {
		t.Fatalf("link not remembered: %v", err)
	}
	if code, status := do(http.MethodDelete, "mewla-friends:DELETE:/friends", nil); code != http.StatusOK || status.Linked {
		t.Fatalf("forget: %d %+v", code, status)
	}
	if code, _ := do(http.MethodPost, post([]byte(`{}`)), []byte(`{}`)); code != http.StatusBadRequest {
		t.Fatalf("link without a code: %d", code)
	}
}
