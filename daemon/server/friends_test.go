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
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/friendsvc"
	"github.com/daoleno/mewla/daemon/push"
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
	swapped, _ := json.Marshal(map[string]any{"op": "delete_account"})
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

	// Turning off only stops Friends here: the person, the sign-in and the
	// drafts stay, and nothing on this computer acts for Friends.
	keptDraft, err := local.Submit(context.Background(), "A draft that waits through the off switch.", "")
	if err != nil || keptDraft.DraftID == "" {
		t.Fatalf("second submit = %+v %v", keptDraft, err)
	}
	if code, raw := act(map[string]any{"op": "off"}); code != http.StatusOK {
		t.Fatalf("disable: %d %s", code, raw)
	}
	if st := status(); st.Enabled || st.Handle != "owner" || !st.SignedIn || len(st.Drafts) != 0 || st.Me != nil {
		t.Fatalf("after off: %+v", st)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM people`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("server has %d people after off (%v)", count, err)
	}
	if _, err := local.Submit(context.Background(), "Nothing drafts while it is off.", ""); !errors.Is(err, friends.ErrNotLinked) {
		t.Fatalf("submit while off = %v", err)
	}
	for _, op := range []map[string]any{
		{"op": "call", "method": "GET", "path": "/feed"},
		{"op": "auto_post", "enabled": true, "tz": "UTC"},
		{"op": "post_draft", "draft_id": keptDraft.DraftID},
		{"op": "discard_draft", "draft_id": keptDraft.DraftID},
		{"op": "draft_media", "draft_id": keptDraft.DraftID, "file": "x.png"},
		{"op": "draft_media_remove", "draft_id": keptDraft.DraftID, "index": 0},
	} {
		if code, raw := act(op); code != http.StatusConflict || !strings.Contains(string(raw), "not_enabled") {
			t.Errorf("%v while off = %d %s", op["op"], code, raw)
		}
	}
	if drafts, _ := friends.NewStore(manager.StorageDir()).Drafts(); len(drafts) != 1 {
		t.Fatalf("drafts after off = %+v", drafts)
	}

	// The old "disable" meant delete everything; an App that still sends it
	// is refused rather than answered with the wrong meaning.
	if code, _ := act(map[string]any{"op": "disable"}); code != http.StatusBadRequest {
		t.Fatalf("old disable op = %d, want 400", code)
	}

	// Turning it back on resumes the same person with its draft.
	if code, raw := act(map[string]any{"op": "enable", "agent_name": "Mochi", "pet_id": "cat", "tz": "UTC"}); code != http.StatusOK {
		t.Fatalf("enable again: %d %s", code, raw)
	}
	if st := status(); !st.Enabled || st.Handle != "owner" || len(st.Drafts) != 1 || st.Me == nil {
		t.Fatalf("after on again: %+v", st)
	}

	// Deleting the account is its own operation: it removes the person and
	// clears this computer, which leaves Friends off.
	if code, raw := act(map[string]any{"op": "delete_account"}); code != http.StatusOK {
		t.Fatalf("delete_account: %d %s", code, raw)
	}
	if st := status(); st.Enabled || st.Handle != "" || len(st.Drafts) != 0 {
		t.Fatalf("after delete: %+v", st)
	}
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

// The phone sees a draft's pictures, removes one before posting, and turns on
// media for auto-post, all through the computer; the video file itself never
// goes to the phone.
func TestFriendsDraftMediaThroughThePairedPhone(t *testing.T) {
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	store, err := friendsvc.OpenStore(filepath.Join(t.TempDir(), "friends.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	media := &friendsvc.DirMediaStore{Dir: filepath.Join(t.TempDir(), "media")}
	service, err := friendsvc.New(friendsvc.Config{Store: store, Media: media, TempDir: t.TempDir(), Accounts: theComputersAccount{
		key: manager.PublicKeyHex(), account: friendsvc.Account{ID: "acct", Login: "owner", CreatedAt: time.Now().Add(-400 * 24 * time.Hour)}}})
	if err != nil {
		t.Fatal(err)
	}
	hosted := httptest.NewServer(service.Handler())
	defer hosted.Close()
	media.PublicURL = hosted.URL + friends.MountPrefix
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	act := func(op map[string]any) (int, []byte) {
		body, _ := json.Marshal(op)
		sum := sha256.Sum256(body)
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		purpose := "mewla-friends:POST:/friends:sha256=" + hex.EncodeToString(sum[:])
		signature := ed25519.Sign(privateKey, auth.BuildSignaturePayload(purpose, manager.DaemonID(), deviceID, timestamp, hex.EncodeToString(nonce)))
		request := httptest.NewRequest(http.MethodPost, "/friends", bytes.NewReader(body))
		request.Header.Set("Authorization", auth.AuthorizationHeaderPrefix+"v1:"+deviceID+":"+manager.DaemonID()+":"+timestamp+":"+hex.EncodeToString(nonce)+":"+hex.EncodeToString(signature))
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		return response.Code, response.Body.Bytes()
	}
	if err := remote.SaveConfig(manager.StorageDir(), remote.Config{ServiceURL: hosted.URL, Machine: remote.Machine{
		AccountLogin: "owner", Hostname: "h.example.test", Label: "h", RouteID: "r", Relays: []remote.Relay{{Name: "x"}}}}); err != nil {
		t.Fatal(err)
	}
	if code, raw := act(map[string]any{"op": "enable", "agent_name": "Mochi", "tz": "UTC"}); code != http.StatusOK {
		t.Fatalf("enable: %d %s", code, raw)
	}

	picture := t.TempDir() + "/a.png"
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 20, 10)))
	if err := os.WriteFile(picture, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	local := &friends.Local{StateDir: manager.StorageDir(), Auth: manager}
	out, err := local.SubmitMedia(context.Background(), "I drew a small square.", "", []string{picture, picture})
	if err != nil || out.DraftID == "" {
		t.Fatalf("submit = %+v %v", out, err)
	}
	draft, err := friends.NewStore(manager.StorageDir()).Draft(out.DraftID)
	if err != nil || len(draft.Media) != 2 {
		t.Fatalf("draft = %+v %v", draft, err)
	}

	// The phone fetches a draft picture by a file name the draft lists, and
	// nothing else (no path walks, no other files on the computer).
	code, raw := act(map[string]any{"op": "draft_media", "draft_id": out.DraftID, "file": draft.Media[0].File})
	if code != http.StatusOK || !strings.Contains(string(raw), `"mime":"image/png"`) || !strings.Contains(string(raw), `"data":"`) {
		t.Fatalf("draft_media = %d %s", code, raw)
	}
	for _, file := range []string{"../../state.json", "/etc/passwd", "", "nope.png"} {
		if code, _ := act(map[string]any{"op": "draft_media", "draft_id": out.DraftID, "file": file}); code != http.StatusNotFound {
			t.Errorf("draft_media %q = %d, want 404", file, code)
		}
	}

	if code, raw := act(map[string]any{"op": "draft_media_remove", "draft_id": out.DraftID, "index": 0}); code != http.StatusOK || !strings.Contains(string(raw), out.DraftID) {
		t.Fatalf("remove = %d %s", code, raw)
	}
	if left, _ := friends.NewStore(manager.StorageDir()).Draft(out.DraftID); len(left.Media) != 1 || left.Media[0].File != draft.Media[1].File {
		t.Fatalf("after removal: %+v", left.Media)
	}

	on := true
	// The media choice is sent only when the phone sent it.
	if body := autoPostBody(friendsAction{Enabled: &on, TZ: "UTC"}); body["media"] != nil {
		t.Fatalf("an old phone's request carries media: %v", body)
	}
	if body := autoPostBody(friendsAction{Media: &on}); body["media"] != true || body["enabled"] != nil {
		t.Fatalf("a media-only request must leave auto-post alone: %v", body)
	}
	if code, raw := act(map[string]any{"op": "auto_post", "enabled": true, "media": true, "tz": "UTC"}); code != http.StatusOK || !strings.Contains(string(raw), `"auto_media":true`) {
		t.Fatalf("auto_post = %d %s", code, raw)
	}
	// A phone that did not show the media (an older App) cannot post it.
	if code, raw := act(map[string]any{"op": "post_draft", "draft_id": out.DraftID}); code != http.StatusConflict || !strings.Contains(string(raw), "media_not_reviewed") {
		t.Fatalf("post without review: %d %s", code, raw)
	}
	if code, raw := act(map[string]any{"op": "post_draft", "draft_id": out.DraftID, "reviewed": []string{draft.Media[1].File}}); code != http.StatusOK {
		t.Fatalf("post draft: %d %s", code, raw)
	}
}

type recordingFriendsPusher struct {
	notificationPusher
	sent []push.Message
}

func (p *recordingFriendsPusher) Send(message push.Message) error {
	p.sent = append(p.sent, message)
	return nil
}

// Friends turned off is silent: no push about drafts that are kept, and the
// count the phone was last told does not move.
func TestFriendsNotifierIsSilentWhileOff(t *testing.T) {
	manager, _, _ := sessionFileAuthFixture(t)
	srv := New(manager, nil, nil, nil, nil, nil, nil)
	pusher := &recordingFriendsPusher{}
	srv.pusher = pusher
	store := friends.NewStore(manager.StorageDir())
	if err := store.SaveState(friends.State{Handle: "owner", EnabledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddDraft("A waiting draft.", ""); err != nil {
		t.Fatal(err)
	}

	local := srv.friendsLocal()
	if err := local.Disable(); err != nil {
		t.Fatal(err)
	}
	srv.friendsNotifyOnce(context.Background())
	if len(pusher.sent) != 0 {
		t.Fatalf("pushed while off: %+v", pusher.sent)
	}
	if state, err := store.Account(); err != nil || state.DraftsPushed != 0 {
		t.Fatalf("state moved while off: %+v %v", state, err)
	}

	state, _ := store.Account()
	state.Off = false
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	srv.friendsNotifyOnce(context.Background())
	if len(pusher.sent) != 1 || !strings.Contains(pusher.sent[0].Body, "1 draft") {
		t.Fatalf("on again: %+v", pusher.sent)
	}
}
