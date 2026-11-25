package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/usage/ingest"
)

func TestUsageMountsBesideAccountAndNeverShadowsIt(t *testing.T) {
	store, err := ingest.OpenStore(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	root := withUsage(account, ingest.New(ingest.Config{Store: store}))
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/v1/machine", http.StatusTeapot},
		{http.MethodPost, "/v1/login/start", http.StatusTeapot},
		{http.MethodGet, "/device", http.StatusTeapot},
		{http.MethodGet, "/v1/events", http.StatusTeapot}, // only POST is usage's
		{http.MethodPost, "/v1/events", http.StatusBadRequest},
		{http.MethodPost, "/v1/visit", http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req.RemoteAddr = "203.0.113.9:1"
		root.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}
