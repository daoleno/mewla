package friends_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/friendsvc"
)

type oneAccount struct {
	key     string
	account friendsvc.Account
}

func (o oneAccount) AccountForKey(_ context.Context, key string) (friendsvc.Account, bool, error) {
	return o.account, key == o.key, nil
}

type fixture struct {
	local  *friends.Local
	client *friends.Client
	other  *friends.Client // a second person, to look at the feed from outside
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := friendsvc.OpenStore(filepath.Join(t.TempDir(), "friends.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	stateDir := t.TempDir()
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	service, err := friendsvc.New(friendsvc.Config{
		Store: store,
		Accounts: oneAccount{key: manager.PublicKeyHex(), account: friendsvc.Account{
			ID: "acct-1", Login: "alice", CreatedAt: time.Now().Add(-400 * 24 * time.Hour)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	client := &friends.Client{BaseURL: server.URL + friends.MountPrefix, Auth: manager}
	local := &friends.Local{StateDir: stateDir, Auth: manager, NewClient: func() (*friends.Client, error) { return client, nil }}
	return &fixture{local: local, client: client}
}

func TestEnableSubmitPostAndTurnOff(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t.Setenv("HOME", t.TempDir())

	if _, err := f.local.Submit(ctx, "Fixed a flaky test.", ""); !errors.Is(err, friends.ErrNotLinked) {
		t.Fatalf("submit while off = %v", err)
	}
	me, err := f.local.Enable(ctx, "Mochi", "cat", "UTC")
	if err != nil || me.Handle != "alice" {
		t.Fatalf("enable = %+v, %v", me, err)
	}
	if state, ok := f.local.Enabled(); !ok || state.Handle != "alice" {
		t.Fatalf("state = %+v %v", state, ok)
	}

	// Auto-post is off: the text is kept, not sent.
	outcome, err := f.local.Submit(ctx, "Fixed a flaky test by waiting for the right event.", "Work: flaky test")
	if err != nil || outcome.Posted || outcome.DraftID == "" {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	// The scrub refuses what the server would not take either.
	if _, err := f.local.Submit(ctx, "mail me at someone@example.com", ""); err == nil {
		t.Fatal("scrub accepted an email")
	} else {
		var refused *friends.RefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("error = %v", err)
		}
	}

	// The owner edits the draft; the edit is scrubbed again, then posts.
	if _, err := f.local.PostDraft(ctx, outcome.DraftID, "now reach me at someone@example.com"); err == nil {
		t.Fatal("edited draft skipped the scrub")
	}
	post, err := f.local.PostDraft(ctx, outcome.DraftID, "Fixed a flaky test by waiting for the event, not a timer.")
	if err != nil || !strings.Contains(post.Text, "not a timer") {
		t.Fatalf("post = %+v, %v", post, err)
	}
	if err := f.local.Discard(outcome.DraftID); !errors.Is(err, friends.ErrNoDraft) {
		t.Fatalf("draft still there: %v", err)
	}

	// With auto-post on, the next one goes straight out.
	if err := f.client.Do(ctx, "PUT", "/me/auto-post", map[string]any{"enabled": true}, nil); err != nil {
		t.Fatal(err)
	}
	auto, err := f.local.Submit(ctx, "Auto posts need a daily cap.", "")
	if err != nil || !auto.Posted || auto.PostID == "" {
		t.Fatalf("auto = %+v, %v", auto, err)
	}
	for i := 0; i < 2; i++ {
		if out, err := f.local.Submit(ctx, "Another small win number "+string(rune('a'+i)), ""); err != nil || !out.Posted {
			t.Fatalf("auto %d = %+v, %v", i, out, err)
		}
	}
	// The cap is used up: the fourth is kept as a draft with the reason.
	capped, err := f.local.Submit(ctx, "A fourth thought for today.", "")
	if err != nil || capped.Posted || capped.DraftID == "" || capped.Why == "" {
		t.Fatalf("over the cap = %+v, %v", capped, err)
	}
	// The owner can delete an auto post afterwards.
	if err := f.client.DeletePost(ctx, auto.PostID); err != nil {
		t.Fatal(err)
	}

	// Turning off removes the person on the server and everything here.
	if err := f.local.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.local.Enabled(); ok {
		t.Fatal("still on locally")
	}
	if _, err := f.client.Me(ctx); err == nil {
		t.Fatal("server still has the person")
	}
	if _, err := f.local.Submit(ctx, "after off", ""); !errors.Is(err, friends.ErrNotLinked) {
		t.Fatalf("submit after off = %v", err)
	}
}

func TestDisableKeepsLocalStateWhenTheServerCannotBeReached(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t.Setenv("HOME", t.TempDir())
	if _, err := f.local.Enable(ctx, "Mochi", "cat", "UTC"); err != nil {
		t.Fatal(err)
	}
	f.local.NewClient = func() (*friends.Client, error) { return nil, friends.ErrSignedOut }
	if err := f.local.Disable(ctx); !errors.Is(err, friends.ErrSignedOut) {
		t.Fatalf("disable signed out = %v", err)
	}
	if _, ok := f.local.Enabled(); !ok {
		t.Fatal("local state was dropped although the server still has the person")
	}
}

func TestAutoPostFallsBackToADraftWhenTheServiceIsDown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t.Setenv("HOME", t.TempDir())
	if _, err := f.local.Enable(ctx, "Mochi", "cat", "UTC"); err != nil {
		t.Fatal(err)
	}
	f.local.NewClient = func() (*friends.Client, error) {
		return &friends.Client{BaseURL: "http://127.0.0.1:1/friends", Auth: f.client.Auth}, nil
	}
	out, err := f.local.Submit(ctx, "The service is down but the thought stays.", "")
	if err != nil || out.Posted || out.DraftID == "" {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
}
