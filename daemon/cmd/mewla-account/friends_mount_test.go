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
	handler, closeFriends, err := friendsHandler("", noAccounts{})
	if err != nil || handler != nil {
		t.Fatalf("unconfigured = %v, %v", handler, err)
	}
	closeFriends()
}

func TestFriendsMountsBesideAccountAndNeverShadowsIt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state", "friends.db")
	friends, closeFriends, err := friendsHandler(db, noAccounts{})
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
