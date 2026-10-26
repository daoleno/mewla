package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func envOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestFriendsIsOffUnlessConfigured(t *testing.T) {
	handler, closeFriends, err := friendsHandler("", false, "https://connect.example", envOf(nil))
	if err != nil || handler != nil {
		t.Fatalf("unconfigured = %v, %v", handler, err)
	}
	closeFriends()
}

func TestFriendsRefusesHalfSetConfiguration(t *testing.T) {
	db := filepath.Join(t.TempDir(), "friends.db")
	cases := map[string]struct {
		db   string
		fake bool
		env  map[string]string
	}{
		"settings without a database": {"", false, map[string]string{"MEWLA_ACCOUNT_FRIENDS_GITHUB_CLIENT_ID": "id"}},
		"fake without a database":     {"", true, nil},
		"database without sign-in":    {db, false, nil},
		"id without secret":           {db, false, map[string]string{"MEWLA_ACCOUNT_FRIENDS_GITHUB_CLIENT_ID": "id"}},
		"fake and real together": {db, true, map[string]string{
			"MEWLA_ACCOUNT_FRIENDS_GITHUB_CLIENT_ID": "id", "MEWLA_ACCOUNT_FRIENDS_GITHUB_CLIENT_SECRET": "s"}},
	}
	for name, c := range cases {
		if handler, _, err := friendsHandler(c.db, c.fake, "https://connect.example", envOf(c.env)); err == nil || handler != nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFriendsMountsBesideAccountAndNeverShadowsIt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state", "friends.db")
	friends, closeFriends, err := friendsHandler(db, true, "http://127.0.0.1:1", envOf(nil))
	if err != nil || friends == nil {
		t.Fatal(err)
	}
	defer closeFriends()
	account := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	root := withFriends(account, friends)
	for path, want := range map[string]int{
		"/v1/machine":              http.StatusTeapot, // account keeps its routes
		"/healthz":                 http.StatusTeapot,
		"/friends/v1/me":           http.StatusUnauthorized, // friends answers its own
		"/friends/v1/enroll/start": http.StatusBadRequest,
		"/friends/v1/not-a-route":  http.StatusNotFound,
		"/friendship":              http.StatusTeapot,
	} {
		recorder := httptest.NewRecorder()
		root.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != want {
			t.Errorf("%s = %d, want %d", path, recorder.Code, want)
		}
	}
}
