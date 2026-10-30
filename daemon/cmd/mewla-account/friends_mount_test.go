package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/daoleno/mewla/daemon/friendsvc"
)

type noAccounts struct{}

func (noAccounts) AccountForKey(context.Context, string) (friendsvc.Account, bool, error) {
	return friendsvc.Account{}, false, nil
}

func TestFriendsIsOffUnlessConfigured(t *testing.T) {
	handler, closeFriends, err := friendsHandler(context.Background(), "", friendsMediaFlags{}, "", noAccounts{})
	if err != nil || handler != nil {
		t.Fatalf("unconfigured = %v, %v", handler, err)
	}
	closeFriends()
}

func TestFriendsMountsBesideAccountAndNeverShadowsIt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state", "friends.db")
	friends, closeFriends, err := friendsHandler(context.Background(), db, friendsMediaFlags{}, "", noAccounts{})
	if err != nil || friends == nil {
		t.Fatal(err)
	}
	defer closeFriends()
	account := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	root := withFriends(account, friends)
	for path, want := range map[string]int{
		"/v1/machine":             http.StatusTeapot, // account keeps its routes
		"/healthz":                http.StatusTeapot,
		"/friends/v1/me":          http.StatusUnauthorized, // friends answers its own, and wants a signature
		"/friends/v1/not-a-route": http.StatusNotFound,
		"/friendship":             http.StatusTeapot,
	} {
		recorder := httptest.NewRecorder()
		root.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != want {
			t.Errorf("%s = %d, want %d", path, recorder.Code, want)
		}
	}
}

func TestFriendsMediaStoreChoices(t *testing.T) {
	t.Setenv("MEWLA_ACCOUNT_MEDIA_ACCESS_KEY", "")
	t.Setenv("MEWLA_ACCOUNT_MEDIA_SECRET_KEY", "")
	if store, err := (friendsMediaFlags{}).store("https://x"); store != nil || err != nil {
		t.Fatalf("no flags = %v, %v", store, err)
	}
	// A half-set object store is an error, never a quiet fallback to no media.
	if _, err := (friendsMediaFlags{bucket: "b"}).store("https://x"); err == nil {
		t.Fatal("half-configured object store was accepted")
	}
	t.Setenv("MEWLA_ACCOUNT_MEDIA_ACCESS_KEY", "k")
	t.Setenv("MEWLA_ACCOUNT_MEDIA_SECRET_KEY", "s")
	full := friendsMediaFlags{endpoint: "https://e", bucket: "b", publicURL: "https://m"}
	if store, err := full.store(""); err != nil || store == nil {
		t.Fatalf("object store = %v, %v", store, err)
	}
	if _, err := (friendsMediaFlags{dir: "d", endpoint: "https://e", bucket: "b", publicURL: "https://m"}).store("https://x"); err == nil {
		t.Fatal("both stores accepted")
	}
	if _, err := (friendsMediaFlags{dir: t.TempDir()}).store(""); err == nil {
		t.Fatal("directory store without a public URL accepted")
	}
}
