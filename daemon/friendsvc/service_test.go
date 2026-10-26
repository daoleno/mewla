package friendsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/reqsign"
)

type fakeAccounts struct {
	mu       sync.Mutex
	accounts map[string]Account
}

func (f *fakeAccounts) AccountForKey(_ context.Context, key string) (Account, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[key]
	return a, ok, nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type user struct {
	h       *harness
	manager *auth.Manager
	account Account
	me      meView
}

type harness struct {
	t        *testing.T
	store    *Store
	accounts *fakeAccounts
	clock    *clock
	server   *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "friends.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h := &harness{t: t, store: store, accounts: &fakeAccounts{accounts: map[string]Account{}},
		clock: &clock{t: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}}
	service, err := New(Config{Store: store, Accounts: h.accounts, Now: h.clock.Now, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewServer(service.Handler())
	t.Cleanup(h.server.Close)
	return h
}

// signIn makes a computer signed in to a new account of the given age.
func (h *harness) signIn(login string, age time.Duration) *user {
	h.t.Helper()
	manager, err := auth.NewManager(h.t.TempDir())
	if err != nil {
		h.t.Fatal(err)
	}
	account := Account{ID: "acct-" + login, Login: login, CreatedAt: h.clock.Now().Add(-age)}
	h.accounts.mu.Lock()
	h.accounts.accounts[manager.PublicKeyHex()] = account
	h.accounts.mu.Unlock()
	return &user{h: h, manager: manager, account: account}
}

func (u *user) call(method, path string, body any) (int, []byte) {
	return u.callWith(u.manager, method, path, body)
}

func (u *user) callWith(signer *auth.Manager, method, path string, body any) (int, []byte) {
	u.h.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	full := apiPrefix + path
	query := ""
	if i := strings.Index(full, "?"); i >= 0 {
		full, query = full[:i], full[i:]
	}
	timestamp := strconv.FormatInt(u.h.clock.Now().UnixMilli(), 10)
	nonce, _ := randomHex(16)
	signature := signer.CreateLinkSignature(reqsign.AccountScheme.Payload(method, full, timestamp, nonce, raw))
	request, _ := http.NewRequest(method, u.h.server.URL+full+query, bytes.NewReader(raw))
	request.Header.Set("Authorization", reqsign.AccountScheme.Header(signer.PublicKeyHex(), timestamp, nonce, signature))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		u.h.t.Fatal(err)
	}
	defer response.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(response.Body)
	return response.StatusCode, buf.Bytes()
}

func (u *user) ok(method, path string, body any, out any) {
	u.h.t.Helper()
	status, raw := u.call(method, path, body)
	if status/100 != 2 {
		u.h.t.Fatalf("%s %s = %d %s", method, path, status, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			u.h.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
}

func (u *user) expect(want int, method, path string, body any) []byte {
	u.h.t.Helper()
	status, raw := u.call(method, path, body)
	if status != want {
		u.h.t.Fatalf("%s %s = %d %s, want %d", method, path, status, raw, want)
	}
	return raw
}

func (u *user) enable() {
	u.h.t.Helper()
	u.ok("POST", "/me/enable", map[string]string{"agent_name": u.account.Login + "'s agent", "pet_id": "cat", "tz": "UTC"}, &u.me)
}

type feed struct {
	Posts []postView `json:"posts"`
	Next  string     `json:"next"`
}

func (u *user) feed(query string) []postView {
	u.h.t.Helper()
	var f feed
	u.ok("GET", "/feed"+query, nil, &f)
	return f.Posts
}

func (u *user) post(text string) postView {
	u.h.t.Helper()
	var p postView
	u.ok("POST", "/posts", map[string]string{"text": text, "via": "approved"}, &p)
	return p
}

func ids(posts []postView) []string {
	out := []string{}
	for _, p := range posts {
		out = append(out, p.ID)
	}
	return out
}

func TestEveryRouteWantsASignedInComputer(t *testing.T) {
	h := newHarness(t)
	stranger, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := h.signIn("alice", 400*24*time.Hour)
	a.enable()
	routes := []struct{ method, path string }{
		{"GET", "/me"}, {"DELETE", "/me"}, {"PATCH", "/me/profile"}, {"PUT", "/me/auto-post"}, {"POST", "/me/enable"},
		{"GET", "/feed"}, {"POST", "/posts"}, {"GET", "/posts/x"}, {"DELETE", "/posts/x"},
		{"PUT", "/posts/x/like"}, {"DELETE", "/posts/x/like"}, {"PUT", "/posts/x/bookmark"}, {"DELETE", "/posts/x/bookmark"},
		{"GET", "/bookmarks"}, {"POST", "/posts/x/comments"}, {"DELETE", "/comments/x"},
		{"GET", "/people/x"}, {"GET", "/people/x/posts"}, {"PUT", "/people/x/follow"}, {"DELETE", "/people/x/follow"},
		{"PUT", "/people/x/block"}, {"DELETE", "/people/x/block"}, {"GET", "/blocks"}, {"POST", "/reports"},
		{"GET", "/notifications"}, {"POST", "/notifications/read"},
	}
	for _, route := range routes {
		// Unsigned.
		request, _ := http.NewRequest(route.method, h.server.URL+apiPrefix+route.path, nil)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("unsigned %s %s = %d", route.method, route.path, response.StatusCode)
		}
		// Signed by a key that is not signed in to any account.
		if status, _ := a.callWith(stranger, route.method, route.path, nil); status != http.StatusUnauthorized {
			t.Errorf("stranger %s %s = %d", route.method, route.path, status)
		}
	}
}

func TestReplayAndStaleSignaturesAreRefused(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	timestamp := strconv.FormatInt(h.clock.Now().UnixMilli(), 10)
	nonce, _ := randomHex(16)
	path := apiPrefix + "/me/enable"
	send := func(ts string) int {
		signature := a.manager.CreateLinkSignature(reqsign.AccountScheme.Payload("POST", path, ts, nonce, nil))
		request, _ := http.NewRequest("POST", h.server.URL+path, nil)
		request.Header.Set("Authorization", reqsign.AccountScheme.Header(a.manager.PublicKeyHex(), ts, nonce, signature))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if got := send(timestamp); got != http.StatusOK {
		t.Fatalf("first = %d", got)
	}
	if got := send(timestamp); got != http.StatusUnauthorized {
		t.Fatalf("replay = %d", got)
	}
	old := strconv.FormatInt(h.clock.Now().Add(-time.Hour).UnixMilli(), 10)
	nonce, _ = randomHex(16)
	if got := send(old); got != http.StatusUnauthorized {
		t.Fatalf("expired = %d", got)
	}
}

func TestEnablingNeedsAnOldEnoughAccountAndNotADisabledOne(t *testing.T) {
	h := newHarness(t)
	young := h.signIn("newbie", 10*24*time.Hour)
	raw := young.expect(http.StatusForbidden, "POST", "/me/enable", nil)
	if !strings.Contains(string(raw), "account_too_new") {
		t.Fatalf("young = %s", raw)
	}
	young.expect(http.StatusNotFound, "GET", "/me", nil)
	// A month later the same account may.
	h.clock.Add(21 * 24 * time.Hour)
	young.enable()

	banned := h.signIn("banned", 400*24*time.Hour)
	h.accounts.mu.Lock()
	for k, a := range h.accounts.accounts {
		if a.Login == "banned" {
			a.Disabled = true
			h.accounts.accounts[k] = a
		}
	}
	h.accounts.mu.Unlock()
	banned.expect(http.StatusForbidden, "POST", "/me/enable", nil)
}

func TestEnableIsIdempotentAndComputersShareOnePerson(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	a.enable()
	// A second computer on the same account.
	second, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.accounts.mu.Lock()
	h.accounts.accounts[second.PublicKeyHex()] = a.account
	h.accounts.mu.Unlock()
	status, raw := a.callWith(second, "POST", "/me/enable", map[string]string{"agent_name": "other"})
	if status != 200 {
		t.Fatalf("second enable = %d %s", status, raw)
	}
	var again meView
	_ = json.Unmarshal(raw, &again)
	if again.ID != a.me.ID || again.AgentName != a.me.AgentName {
		t.Fatalf("second computer made another person: %+v vs %+v", again, a.me)
	}
	if a.me.Handle != "alice" {
		t.Fatalf("handle = %q", a.me.Handle)
	}
	// A post from either computer is the same person's.
	var p postView
	status, raw = a.callWith(second, "POST", "/posts", map[string]string{"text": "from the other computer", "via": "approved"})
	if status != 201 {
		t.Fatalf("post = %d %s", status, raw)
	}
	_ = json.Unmarshal(raw, &p)
	if p.Author.ID != a.me.ID {
		t.Fatal("posted as a different person")
	}
}

func TestHandleFollowsTheLoginAndChangesOnce(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("Alice", 400*24*time.Hour)
	a.enable()
	if a.me.Handle != "alice" {
		t.Fatalf("handle = %q", a.me.Handle)
	}
	twin := h.signIn("alice!", 400*24*time.Hour)
	twin.account.Login = "alice"
	twin.enable()
	if twin.me.Handle == "alice" || !strings.HasPrefix(twin.me.Handle, "alice") {
		t.Fatalf("clash handle = %q", twin.me.Handle)
	}
	var me meView
	a.ok("PATCH", "/me/profile", map[string]string{"handle": "ali-cat"}, &me)
	if me.Handle != "ali-cat" || !me.HandleChanged {
		t.Fatalf("after change = %+v", me)
	}
	a.expect(http.StatusConflict, "PATCH", "/me/profile", map[string]string{"handle": "another"})
	twin.expect(http.StatusConflict, "PATCH", "/me/profile", map[string]string{"handle": "ali-cat"})
	twin.expect(http.StatusBadRequest, "PATCH", "/me/profile", map[string]string{"handle": "No Spaces"})
}

func TestPostingFeedFollowingCommentsLikesBookmarks(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	b := h.signIn("bob", 400*24*time.Hour)
	a.enable()
	b.enable()

	post := a.post("Shipped the thing today.")
	if post.Author.Handle != "alice" || !post.Mine {
		t.Fatalf("post = %+v", post)
	}
	// B sees it in All but not in Following until following.
	if got := ids(b.feed("")); len(got) != 1 || got[0] != post.ID {
		t.Fatalf("B all = %v", got)
	}
	if got := b.feed("?scope=following"); len(got) != 0 {
		t.Fatalf("B following before follow = %v", ids(got))
	}
	var person personView
	b.ok("PUT", "/people/"+a.me.ID+"/follow", nil, &person)
	if !person.Followed || person.Followers != 1 {
		t.Fatalf("person = %+v", person)
	}
	if got := ids(b.feed("?scope=following")); len(got) != 1 {
		t.Fatalf("B following = %v", got)
	}
	// Following by handle works for lookup too.
	b.ok("GET", "/people/alice", nil, &person)
	if person.ID != a.me.ID || person.Posts != 1 {
		t.Fatalf("by handle = %+v", person)
	}

	var liked postView
	b.ok("PUT", "/posts/"+post.ID+"/like", nil, &liked)
	b.ok("PUT", "/posts/"+post.ID+"/like", nil, &liked) // idempotent
	if liked.Likes != 1 || !liked.Liked {
		t.Fatalf("liked = %+v", liked)
	}
	b.ok("PUT", "/posts/"+post.ID+"/bookmark", nil, nil)
	var marks feed
	b.ok("GET", "/bookmarks", nil, &marks)
	if len(marks.Posts) != 1 || !marks.Posts[0].Bookmarked {
		t.Fatalf("bookmarks = %+v", marks)
	}
	var comment commentView
	b.ok("POST", "/posts/"+post.ID+"/comments", map[string]string{"text": "Nice one."}, &comment)

	var detail struct {
		Post     postView      `json:"post"`
		Comments []commentView `json:"comments"`
	}
	a.ok("GET", "/posts/"+post.ID, nil, &detail)
	if detail.Post.Comments != 1 || detail.Post.Likes != 1 || len(detail.Comments) != 1 || detail.Comments[0].Author.Handle != "bob" {
		t.Fatalf("detail = %+v", detail)
	}

	// A is told about the follow, the like and the comment (liking twice once).
	var notes struct {
		Notifications []notificationView `json:"notifications"`
		Unread        int                `json:"unread"`
	}
	a.ok("GET", "/notifications", nil, &notes)
	kinds := map[string]int{}
	for _, n := range notes.Notifications {
		kinds[n.Kind]++
		if n.Actor.Handle != "bob" {
			t.Fatalf("actor = %+v", n.Actor)
		}
	}
	if kinds["follow"] != 1 || kinds["like"] != 1 || kinds["comment"] != 1 || notes.Unread != 3 {
		t.Fatalf("notifications = %v unread %d", kinds, notes.Unread)
	}
	a.ok("POST", "/notifications/read", nil, nil)
	a.ok("GET", "/notifications", nil, &notes)
	if notes.Unread != 0 {
		t.Fatalf("unread after read = %d", notes.Unread)
	}
	var after struct {
		Notifications []notificationView `json:"notifications"`
	}
	a.ok("GET", "/notifications?after="+strconv.FormatInt(notes.Notifications[0].Seq, 10), nil, &after)
	if len(after.Notifications) != 0 {
		t.Fatalf("after newest = %d", len(after.Notifications))
	}

	// Own actions are silent.
	a.ok("PUT", "/posts/"+post.ID+"/like", nil, nil)
	a.expect(http.StatusBadRequest, "PUT", "/people/"+a.me.ID+"/follow", nil)

	// Unlike, unfollow, unbookmark.
	b.ok("DELETE", "/posts/"+post.ID+"/like", nil, nil)
	b.ok("DELETE", "/posts/"+post.ID+"/bookmark", nil, nil)
	b.ok("DELETE", "/people/"+a.me.ID+"/follow", nil, nil)
	if got := b.feed("?scope=following"); len(got) != 0 {
		t.Fatalf("following after unfollow = %d", len(got))
	}
}

func TestDeletingPostsAndComments(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	b := h.signIn("bob", 400*24*time.Hour)
	c := h.signIn("carol", 400*24*time.Hour)
	for _, u := range []*user{a, b, c} {
		u.enable()
	}
	post := a.post("A post.")
	var byB, byC commentView
	b.ok("POST", "/posts/"+post.ID+"/comments", map[string]string{"text": "from bob"}, &byB)
	c.ok("POST", "/posts/"+post.ID+"/comments", map[string]string{"text": "from carol"}, &byC)

	// Only the owner deletes a post.
	b.expect(http.StatusNotFound, "DELETE", "/posts/"+post.ID, nil)
	// A comment's author, or the post's owner, deletes a comment; no one else.
	c.expect(http.StatusNotFound, "DELETE", "/comments/"+byB.ID, nil)
	b.expect(http.StatusOK, "DELETE", "/comments/"+byB.ID, nil)
	a.expect(http.StatusOK, "DELETE", "/comments/"+byC.ID, nil)
	var detail struct {
		Comments []commentView `json:"comments"`
	}
	a.ok("GET", "/posts/"+post.ID, nil, &detail)
	if len(detail.Comments) != 0 {
		t.Fatalf("comments left = %d", len(detail.Comments))
	}
	a.expect(http.StatusOK, "DELETE", "/posts/"+post.ID, nil)
	b.expect(http.StatusNotFound, "GET", "/posts/"+post.ID, nil)
	if got := b.feed(""); len(got) != 0 {
		t.Fatalf("feed after delete = %d", len(got))
	}
}

func TestBlockingHidesBothWaysAndDropsFollows(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	b := h.signIn("bob", 400*24*time.Hour)
	a.enable()
	b.enable()
	aPost, bPost := a.post("alice says hi"), b.post("bob says hi")
	b.ok("PUT", "/people/"+a.me.ID+"/follow", nil, nil)
	a.ok("PUT", "/people/"+b.me.ID+"/follow", nil, nil)
	var comment commentView
	b.ok("POST", "/posts/"+aPost.ID+"/comments", map[string]string{"text": "hello alice"}, &comment)

	b.ok("PUT", "/people/"+a.me.ID+"/block", nil, nil)

	if got := ids(b.feed("")); len(got) != 1 || got[0] != bPost.ID {
		t.Fatalf("B sees %v", got)
	}
	if got := ids(a.feed("")); len(got) != 1 || got[0] != aPost.ID {
		t.Fatalf("A sees %v", got)
	}
	// Neither can fetch the other's person, posts or act on them.
	a.expect(http.StatusNotFound, "GET", "/people/"+b.me.ID, nil)
	a.expect(http.StatusNotFound, "GET", "/posts/"+bPost.ID, nil)
	a.expect(http.StatusNotFound, "PUT", "/posts/"+bPost.ID+"/like", nil)
	a.expect(http.StatusNotFound, "PUT", "/people/"+b.me.ID+"/follow", nil)
	b.expect(http.StatusNotFound, "GET", "/posts/"+aPost.ID, nil)
	b.expect(http.StatusNotFound, "POST", "/posts/"+aPost.ID+"/comments", map[string]string{"text": "again"})
	b.expect(http.StatusNotFound, "PUT", "/people/"+a.me.ID+"/follow", nil)
	// B's earlier comment is gone from A's view of her own post.
	var detail struct {
		Comments []commentView `json:"comments"`
	}
	a.ok("GET", "/posts/"+aPost.ID, nil, &detail)
	if len(detail.Comments) != 0 {
		t.Fatalf("A sees blocked comment: %+v", detail.Comments)
	}
	// The follows are gone both ways, and notifications from the blocked vanish.
	if got := a.feed("?scope=following"); len(got) != 0 {
		t.Fatalf("A following = %d", len(got))
	}
	var notes struct {
		Notifications []notificationView `json:"notifications"`
	}
	a.ok("GET", "/notifications", nil, &notes)
	if len(notes.Notifications) != 0 {
		t.Fatalf("A notifications from blocked B = %d", len(notes.Notifications))
	}
	// B can see who they blocked, and unblocking restores everything.
	var blocks struct {
		People []authorView `json:"people"`
	}
	b.ok("GET", "/blocks", nil, &blocks)
	if len(blocks.People) != 1 || blocks.People[0].ID != a.me.ID {
		t.Fatalf("blocks = %+v", blocks)
	}
	b.ok("DELETE", "/people/"+a.me.ID+"/block", nil, nil)
	if got := b.feed(""); len(got) != 2 {
		t.Fatalf("B after unblock sees %d", len(got))
	}
	if got := a.feed("?scope=following"); len(got) != 0 {
		t.Fatal("follows came back with the unblock")
	}
}

func TestThreeDifferentReportersHideAnItem(t *testing.T) {
	h := newHarness(t)
	author := h.signIn("author", 400*24*time.Hour)
	author.enable()
	post := author.post("a questionable post")
	var comment commentView
	reporters := []*user{}
	for _, name := range []string{"r1", "r2", "r3"} {
		u := h.signIn(name, 400*24*time.Hour)
		u.enable()
		reporters = append(reporters, u)
	}
	reporters[0].ok("POST", "/posts/"+post.ID+"/comments", map[string]string{"text": "a comment"}, &comment)

	report := func(u *user, kind, target string) {
		u.expect(http.StatusOK, "POST", "/reports", map[string]string{"kind": kind, "target_id": target, "reason": "spam"})
	}
	// The same person reporting five times is one report.
	for i := 0; i < 5; i++ {
		report(reporters[0], "post", post.ID)
	}
	report(reporters[1], "post", post.ID)
	if got := reporters[2].feed(""); len(got) != 1 {
		t.Fatalf("hidden after two different reporters: %d", len(got))
	}
	report(reporters[2], "post", post.ID)
	if got := reporters[2].feed(""); len(got) != 0 {
		t.Fatalf("still visible after three: %d", len(got))
	}
	// The author still sees their own post.
	if got := author.feed(""); len(got) != 1 {
		t.Fatalf("author lost their own post: %d", len(got))
	}
	// Reporting yourself is refused; so is a missing target.
	author.expect(http.StatusBadRequest, "POST", "/reports", map[string]string{"kind": "post", "target_id": post.ID})
	reporters[0].expect(http.StatusNotFound, "POST", "/reports", map[string]string{"kind": "post", "target_id": "nope"})
	reporters[0].expect(http.StatusBadRequest, "POST", "/reports", map[string]string{"kind": "banana", "target_id": post.ID})
	// Comments hide the same way.
	report(reporters[1], "comment", comment.ID)
	report(reporters[2], "comment", comment.ID)
	author.expect(http.StatusOK, "POST", "/reports", map[string]string{"kind": "comment", "target_id": comment.ID})
	var detail struct {
		Comments []commentView `json:"comments"`
	}
	author.ok("GET", "/posts/"+post.ID, nil, &detail)
	if len(detail.Comments) != 0 {
		t.Fatalf("hidden comment still shown")
	}
}

func TestAutoPostIsOffByDefaultAndCappedPerDay(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	a.enable()
	auto := func(text, ref string) (int, []byte) {
		return a.call("POST", "/posts", map[string]string{"text": text, "via": "auto", "client_ref": ref})
	}
	if status, _ := auto("no consent yet", "r0"); status != http.StatusForbidden {
		t.Fatalf("auto while off = %d", status)
	}
	var me meView
	a.ok("PUT", "/me/auto-post", map[string]any{"enabled": true, "tz": "UTC"}, &me)
	if !me.AutoPost || me.DailyCap != 3 {
		t.Fatalf("me = %+v", me)
	}
	for i := 1; i <= 3; i++ {
		if status, raw := auto("auto post "+strconv.Itoa(i), "r"+strconv.Itoa(i)); status != http.StatusCreated {
			t.Fatalf("auto %d = %d %s", i, status, raw)
		}
	}
	if status, raw := auto("one too many", "r4"); status != http.StatusTooManyRequests || !strings.Contains(string(raw), "daily_cap") {
		t.Fatalf("fourth = %d %s", status, raw)
	}
	// A retry of a post that already went out is the same post, not a new one.
	status, raw := auto("auto post 2", "r2")
	if status != http.StatusOK {
		t.Fatalf("retry = %d %s", status, raw)
	}
	// Approved posts are not auto posts, but there is a hard cap on the day.
	for i := 0; i < hardDailyPosts-3; i++ {
		a.post("approved " + strconv.Itoa(i))
	}
	a.expect(http.StatusTooManyRequests, "POST", "/posts", map[string]string{"text": "past the hard cap", "via": "approved"})
	// The next day, the cap resets.
	h.clock.Add(24 * time.Hour)
	if status, raw := auto("a new day", "r5"); status != http.StatusCreated {
		t.Fatalf("next day = %d %s", status, raw)
	}
	// Turning auto-post off stops it at once.
	a.ok("PUT", "/me/auto-post", map[string]any{"enabled": false}, nil)
	if status, _ := auto("after off", "r6"); status != http.StatusForbidden {
		t.Fatalf("auto after off = %d", status)
	}
}

func TestTextTheServerWillNotTake(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	a.enable()
	for name, text := range map[string]string{
		"empty":  "   ",
		"long":   strings.Repeat("x", maxPostRunes+1),
		"secret": "key is sk-" + strings.Repeat("a1B2", 12),
		"email":  "mail me at someone@example.com",
	} {
		status, raw := a.call("POST", "/posts", map[string]string{"text": text, "via": "approved"})
		if status != 400 && status != 422 {
			t.Errorf("%s accepted: %d %s", name, status, raw)
		}
	}
	a.expect(http.StatusBadRequest, "POST", "/posts", map[string]string{"text": "fine", "via": "mystery"})
	post := a.post("fine")
	a.expect(http.StatusUnprocessableEntity, "POST", "/posts/"+post.ID+"/comments", map[string]string{"text": "ping me at someone@example.com"})
	a.expect(http.StatusBadRequest, "POST", "/posts/"+post.ID+"/comments", map[string]string{"text": strings.Repeat("y", maxCommentRunes+1)})
}

func TestTurningFriendsOffDeletesEverything(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	b := h.signIn("bob", 400*24*time.Hour)
	c := h.signIn("carol", 400*24*time.Hour)
	for _, u := range []*user{a, b, c} {
		u.enable()
	}
	aPost := a.post("alice's post")
	bPost := b.post("bob's post")
	var aComment, bComment commentView
	a.ok("POST", "/posts/"+bPost.ID+"/comments", map[string]string{"text": "alice on bob"}, &aComment)
	b.ok("POST", "/posts/"+aPost.ID+"/comments", map[string]string{"text": "bob on alice"}, &bComment)
	a.ok("PUT", "/posts/"+bPost.ID+"/like", nil, nil)
	b.ok("PUT", "/posts/"+aPost.ID+"/like", nil, nil)
	a.ok("PUT", "/posts/"+bPost.ID+"/bookmark", nil, nil)
	a.ok("PUT", "/people/"+b.me.ID+"/follow", nil, nil)
	b.ok("PUT", "/people/"+a.me.ID+"/follow", nil, nil)
	a.ok("PUT", "/people/"+c.me.ID+"/block", nil, nil)
	c.expect(http.StatusOK, "POST", "/reports", map[string]string{"kind": "post", "target_id": aPost.ID})
	a.expect(http.StatusOK, "POST", "/reports", map[string]string{"kind": "post", "target_id": bPost.ID})

	a.expect(http.StatusOK, "DELETE", "/me", nil)
	// Alice can come back only by enabling afresh, and starts empty.
	a.expect(http.StatusNotFound, "GET", "/me", nil)
	// Bob sees nothing of alice and no trace in his own things.
	if got := ids(b.feed("")); len(got) != 1 || got[0] != bPost.ID {
		t.Fatalf("bob's feed = %v", got)
	}
	var detail struct {
		Post     postView      `json:"post"`
		Comments []commentView `json:"comments"`
	}
	b.ok("GET", "/posts/"+bPost.ID, nil, &detail)
	if detail.Post.Likes != 0 || detail.Post.Comments != 0 || len(detail.Comments) != 0 {
		t.Fatalf("alice's activity survived: %+v", detail)
	}
	b.expect(http.StatusNotFound, "GET", "/people/"+a.me.ID, nil)

	// The database holds no row that mentions her.
	for table, column := range map[string]string{
		"people": "id", "posts": "person_id", "comments": "person_id", "likes": "person_id", "bookmarks": "person_id",
		"follows": "follower_id", "blocks": "blocker_id", "reports": "reporter_id", "notifications": "person_id",
	} {
		var n int
		if err := h.store.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+column+` = ?`, a.me.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s still has %d rows for alice (%v)", table, n, err)
		}
	}
	for table, column := range map[string]string{"follows": "followee_id", "blocks": "blocked_id", "notifications": "actor_id"} {
		var n int
		if err := h.store.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+column+` = ?`, a.me.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s still mentions alice: %d (%v)", table, n, err)
		}
	}
	var reports int
	_ = h.store.db.QueryRow(`SELECT COUNT(*) FROM reports WHERE target_id = ?`, aPost.ID).Scan(&reports)
	if reports != 0 {
		t.Errorf("reports about alice's post remain")
	}

	// Enabling again is a fresh person with nothing from before.
	a.enable()
	if a.me.ID == "" || len(a.feed("?scope=following")) != 0 {
		t.Fatalf("not fresh: %+v", a.me)
	}
}

func TestProfileEdits(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("alice", 400*24*time.Hour)
	a.enable()
	var me meView
	a.ok("PATCH", "/me/profile", map[string]string{"agent_name": "Pixel", "bio": "Builds small tools.", "pet_id": "fox"}, &me)
	if me.AgentName != "Pixel" || me.Bio != "Builds small tools." || me.PetID != "fox" {
		t.Fatalf("me = %+v", me)
	}
	a.expect(http.StatusBadRequest, "PATCH", "/me/profile", map[string]string{"bio": "mail someone@example.com"})
}
