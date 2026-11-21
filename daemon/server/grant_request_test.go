package server

import (
	"bytes"
	"context"
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
	"github.com/daoleno/mewla/daemon/grants"
	"github.com/daoleno/mewla/daemon/work"
)

type fakeGrantRequests struct {
	calls [][2]string
	err   error
}

func (f *fakeGrantRequests) Act(_ context.Context, id, action string) (work.CodexConversationEvent, error) {
	f.calls = append(f.calls, [2]string{id, action})
	if f.err != nil {
		return work.CodexConversationEvent{}, f.err
	}
	return work.CodexConversationEvent{ID: id, Kind: "status", Role: "assistant", Source: "grant_request", Status: "opened", Body: "A Worker needs to see the screen.", GrantPermission: "screen_recording"}, nil
}

func TestGrantRequestEndpointIsSignedOverItsBodyAndAnswersWithTheCard(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	fake := &fakeGrantRequests{}
	srv.SetGrantRequests(fake)
	do := func(method, purpose string, body []byte) (int, map[string]any) {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		request := httptest.NewRequest(method, "/brain/grant-request", bytes.NewReader(body))
		request.Header.Set("Authorization", auth.AuthorizationHeaderPrefix+"v1:"+deviceID+":"+manager.DaemonID()+":"+timestamp+":"+hex.EncodeToString(nonce)+":"+hex.EncodeToString(signature))
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		var out map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &out)
		return response.Code, out
	}
	purpose := func(body []byte) string {
		sum := sha256.Sum256(body)
		return "mewla-grant-request:POST:/brain/grant-request:sha256=" + hex.EncodeToString(sum[:])
	}
	body := []byte(`{"id":"grant:screen_recording:1","action":"open_settings"}`)

	code, card := do(http.MethodPost, purpose(body), body)
	if code != http.StatusOK || card["source"] != "grant_request" || card["status"] != "opened" || card["grant_permission"] != "screen_recording" || card["id"] != "grant:screen_recording:1" {
		t.Fatalf("%d %v", code, card)
	}
	if len(fake.calls) != 1 || fake.calls[0] != [2]string{"grant:screen_recording:1", "open_settings"} {
		t.Fatalf("calls %v", fake.calls)
	}
	// A signature over another body, or another purpose, does nothing.
	if code, _ := do(http.MethodPost, purpose([]byte(`{"id":"x","action":"not_now"}`)), body); code != http.StatusUnauthorized {
		t.Fatalf("body swapped: %d", code)
	}
	if code, _ := do(http.MethodPost, "mewla-agent-use:POST:/agent-use:sha256="+hex.EncodeToString(sha256.New().Sum(nil)), body); code != http.StatusUnauthorized {
		t.Fatalf("another purpose: %d", code)
	}
	if code, _ := do(http.MethodGet, "mewla-grant-request:GET:/brain/grant-request", nil); code != http.StatusMethodNotAllowed && code != http.StatusUnauthorized {
		t.Fatalf("GET: %d", code)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("a refused request acted: %v", fake.calls)
	}
	empty := []byte(`{"id":""}`)
	if code, out := do(http.MethodPost, purpose(empty), empty); code != http.StatusBadRequest || out["error"] == nil {
		t.Fatalf("%d %v", code, out)
	}

	for _, c := range []struct {
		err  error
		code int
	}{
		{grants.ErrNotFound, http.StatusNotFound},
		{&grants.ConflictError{Message: "This request is granted."}, http.StatusConflict},
		{grants.ErrUnknownAction, http.StatusBadRequest},
		{context.DeadlineExceeded, http.StatusInternalServerError},
	} {
		fake.err = c.err
		code, out := do(http.MethodPost, purpose(body), body)
		if code != c.code || out["error"] == nil || out["error"] == "" {
			t.Fatalf("%v: %d %v", c.err, code, out)
		}
	}
	fake.err = &grants.ConflictError{Message: "This request is granted."}
	if _, out := do(http.MethodPost, purpose(body), body); out["error"] != "This request is granted." {
		t.Fatalf("%v", out)
	}
}

// A card whose status changed is a different event to the feed's diff, so the
// followers of Brain's thread are sent it.
func TestGrantRequestStatusChangesTheFeedFingerprint(t *testing.T) {
	card := work.CodexConversationEvent{ID: "grant:chrome:1", Kind: "status", Source: "grant_request", Status: "asked", Body: "A Worker needs your Chrome.", GrantPermission: "chrome"}
	opened := card
	opened.Status = "opened"
	other := card
	other.GrantPermission = "accessibility"
	base := codexConversationEventsFingerprint([]work.CodexConversationEvent{card})
	if base == codexConversationEventsFingerprint([]work.CodexConversationEvent{opened}) || base == codexConversationEventsFingerprint([]work.CodexConversationEvent{other}) {
		t.Fatal("a status or permission change leaves the fingerprint as it was")
	}
}
