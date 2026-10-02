package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Exercise the actual shared origin used by LAN, reverse proxies and Link.
// Retired destinations must not trigger authentication, consent or startup work.
func TestRetiredRoutesAreNotRegistered(t *testing.T) {
	handler := (&Server{}).Handler()
	for _, path := range []string{"/desktop", "/desktop/capability", "/desktop/scope", "/desktop/moonlight/enroll/begin", "/desktop/moonlight/enroll/complete"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(method+path, func(t *testing.T) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
				if response.Code != http.StatusNotFound {
					t.Fatalf("status = %d, want 404", response.Code)
				}
			})
		}
	}
}
