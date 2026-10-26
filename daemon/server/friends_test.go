package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/friendsvc"
	"github.com/daoleno/mewla/daemon/remote"
)

type theComputersAccount struct {
	key     string
	account friendsvc.Account
}

func (a theComputersAccount) AccountForKey(_ context.Context, key string) (friendsvc.Account, bool, error) {
	return a.account, key == a.key, nil
}

func TestFriendsThroughThePairedPhone(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	store, err := friendsvc.OpenStore(filepath.Join(t.TempDir(), "friends.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := friendsvc.New(friendsvc.Config{Store: store, Accounts: theComputersAccount{
		key: manager.PublicKeyHex(), account: friendsvc.Account{ID: "acct", Login: "owner", CreatedAt: time.Now().Add(-400 * 24 * time.Hour)}}})
	if err != nil {
		t.Fatal(err)
	}
	hosted := httptest.NewServer(service.Handler())
	defer hosted.Close()
	srv := New(manager, nil, nil, nil, nil, nil, nil)

	header := func(purpose string) string {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + manager.DaemonID() + ":" + timestamp + ":" + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(signature)
	}
	do := func(method, purpose string, body []byte) (int, []byte) {
		request := httptest.NewRequest(method, "/friends", bytes.NewReader(body))
		if purpose != "" {
			request.Header.Set("Authorization", header(purpose))
		}
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		return response.Code, response.Body.Bytes()
	}
	act := func(op map[string]any) (int, []byte) {
		body, _ := json.Marshal(op)
		sum := sha256.Sum256(body)
		return do(http.MethodPost, "mewla-friends:POST:/friends:sha256="+hex.EncodeToString(sum[:]), body)
	}
	status := func() friendsStatus {
		code, raw := do(http.MethodGet, "mewla-friends:GET:/friends", nil)
		var out friendsStatus
		if code != http.StatusOK || json.Unmarshal(raw, &out) != nil {
			t.Fatalf("status = %d %s", code, raw)
		}
		return out
	}

	if code, _ := do(http.MethodGet, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unsigned read: %d", code)
	}
	if st := status(); st.SignedIn || st.Enabled {
		t.Fatalf("starts off: %+v", st)
	}
	// A body swapped under a signature is refused.
	swapped, _ := json.Marshal(map[string]any{"op": "disable"})
	sum := sha256.Sum256([]byte(`{"op":"enable"}`))
	if code, _ := do(http.MethodPost, "mewla-friends:POST:/friends:sha256="+hex.EncodeToString(sum[:]), swapped); code != http.StatusUnauthorized {
		t.Fatalf("swapped body: %d", code)
	}
	// Without a Mewla account on this computer, turning on says so.
	if code, raw := act(map[string]any{"op": "enable", "agent_name": "Mochi", "pet_id": "cat"}); code != http.StatusConflict || !strings.Contains(string(raw), "signed_out") {
		t.Fatalf("enable signed out: %d %s", code, raw)
	}
	if code, _ := act(map[string]any{"op": "call", "method": "GET", "path": "/feed"}); code != http.StatusConflict {
		t.Fatalf("call while off: %d", code)
	}

	// Sign this computer in (as `mewla login` would leave it), then turn on.
	if err := remote.SaveConfig(manager.StorageDir(), remote.Config{ServiceURL: hosted.URL, Machine: remote.Machine{
		AccountLogin: "owner", Hostname: "h.example.test", Label: "h", RouteID: "r", Relays: []remote.Relay{{Name: "x"}}}}); err != nil {
		t.Fatal(err)
	}
	if code, raw := act(map[string]any{"op": "enable", "agent_name": "Mochi", "pet_id": "cat", "tz": "UTC"}); code != http.StatusOK {
		t.Fatalf("enable: %d %s", code, raw)
	}
	if st := status(); !st.SignedIn || st.RemoteAccess || !st.Enabled || st.Handle != "owner" || st.Me == nil || st.Me.AgentName != "Mochi" {
		t.Fatalf("after enable: %+v", st)
	}

	// Drafts: the Brain's CLI path puts one in; the phone edits and posts it.
	local := &friends.Local{StateDir: manager.StorageDir(), Auth: manager}
	t.Setenv("HOME", t.TempDir())
	outcome, err := local.Submit(context.Background(), "A small win with the build cache.", "")
	if err != nil || outcome.DraftID == "" {
		t.Fatalf("submit = %+v %v", outcome, err)
	}
	if st := status(); len(st.Drafts) != 1 {
		t.Fatalf("drafts = %+v", st.Drafts)
	}
	if code, raw := act(map[string]any{"op": "post_draft", "draft_id": outcome.DraftID, "text": "reach me at someone@example.com"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("edited draft skipped the scrub: %d %s", code, raw)
	}
	if code, raw := act(map[string]any{"op": "post_draft", "draft_id": outcome.DraftID, "text": "A small win with the build cache, edited."}); code != http.StatusOK {
		t.Fatalf("post draft: %d %s", code, raw)
	}
	if st := status(); len(st.Drafts) != 0 {
		t.Fatalf("draft not consumed: %+v", st.Drafts)
	}

	// The App's screens pass through; turning Friends on or off does not.
	code, raw := act(map[string]any{"op": "call", "method": "GET", "path": "/feed"})
	if code != http.StatusOK || !strings.Contains(string(raw), "build cache, edited") {
		t.Fatalf("feed through the phone: %d %s", code, raw)
	}
	for _, blocked := range []struct{ method, path string }{
		{"DELETE", "/me"}, {"POST", "/me/enable"}, {"PUT", "/me/auto-post"}, {"GET", "/../me"}, {"GET", "/feed/../../x"}, {"GET", "/nothing"},
	} {
		if code, _ := act(map[string]any{"op": "call", "method": blocked.method, "path": blocked.path}); code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", blocked.method, blocked.path, code)
		}
	}

	// Turning off deletes the person and clears this computer.
	if code, raw := act(map[string]any{"op": "disable"}); code != http.StatusOK {
		t.Fatalf("disable: %d %s", code, raw)
	}
	if st := status(); st.Enabled {
		t.Fatalf("still on: %+v", st)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM people`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("server still has %d people (%v)", count, err)
	}
}

func TestFriendsLoginSignsTheComputerIn(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	_ = privateKey
	_ = deviceID
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	srv.friendsLoginPoll(recorder, context.Background(), manager.StorageDir(), "nope")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("poll of an unknown login = %d", recorder.Code)
	}
}

// Signing in for Friends is the Mewla account alone: it must not turn on the
// relay connection, the certificate or the public address. A computer that
// already has remote access keeps it.
func TestFriendsLoginDoesNotTurnOnRemoteAccess(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/login/start":
			_ = json.NewEncoder(w).Encode(map[string]any{"login_id": "l1", "user_code": "ABCD-EFGH", "verification_uri": "https://example.test/device", "interval_seconds": 1})
		case "/v1/login/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "complete", "machine": remote.Machine{
				MachineID: "m1", AccountLogin: "owner", Hostname: "quiet-otter-1a2b.mewla.test", Label: "quiet-otter-1a2b", RouteID: strings.Repeat("ab", 16),
				Relays: []remote.Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 443}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()
	t.Setenv("MEWLA_REMOTE_SERVICE_URL", service.URL)

	signIn := func(manager *auth.Manager) {
		t.Helper()
		srv := New(manager, nil, nil, nil, nil, nil, nil)
		start := httptest.NewRecorder()
		srv.friendsLoginStart(start, context.Background(), manager.StorageDir())
		if start.Code != http.StatusOK {
			t.Fatalf("login start = %d %s", start.Code, start.Body)
		}
		poll := httptest.NewRecorder()
		srv.friendsLoginPoll(poll, context.Background(), manager.StorageDir(), "l1")
		if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"done"`) {
			t.Fatalf("login poll = %d %s", poll.Code, poll.Body)
		}
	}

	fresh, _, _ := sessionFileAuthFixture(t)
	signIn(fresh)
	if account, ok, err := remote.LoadAccount(fresh.StorageDir()); err != nil || !ok || account.Machine.AccountLogin != "owner" || account.RemoteAccess {
		t.Fatalf("account = %+v ok=%v err=%v; want signed in with remote access off", account, ok, err)
	}
	if _, on, _ := remote.LoadRemote(fresh.StorageDir()); on {
		t.Fatal("signing in for Friends turned on remote access")
	}

	connected, _, _ := sessionFileAuthFixture(t)
	if err := remote.SaveConfig(connected.StorageDir(), remote.Config{ServiceURL: service.URL, RemoteAccess: true, Machine: remote.Machine{
		AccountLogin: "owner", Hostname: "h.example.test", Label: "h", RouteID: "r", Relays: []remote.Relay{{Name: "x"}}}}); err != nil {
		t.Fatal(err)
	}
	signIn(connected)
	if _, on, _ := remote.LoadRemote(connected.StorageDir()); !on {
		t.Fatal("a Friends sign-in switched remote access off")
	}
}

func TestFriendsFailLocalErrorIsNotBadGateway(t *testing.T) {
	recorder := httptest.NewRecorder()
	friendsFail(recorder, &localError{errors.New("disk full")})
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"local_error"`) {
		t.Fatalf("local error = %d %s", recorder.Code, recorder.Body)
	}
}
