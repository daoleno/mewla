package account

import (
	"context"
	"net/http"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/reqsign"
)

type daemonKeyContextKey struct{}

// daemonKey returns the verified daemon public key (lower hex) of a request.
func daemonKey(r *http.Request) string {
	key, _ := r.Context().Value(daemonKeyContextKey{}).(string)
	return key
}

// requireSignature authenticates a daemon-signed request and exposes the
// daemon key and body to next.
func (s *Service) requireSignature(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, rejection := s.verifier.Authenticate(w, r)
		if rejection != nil {
			writeError(w, rejection.Status, rejection.Message)
			return
		}
		next(w, reqsign.WithBody(r, context.WithValue(r.Context(), daemonKeyContextKey{}, result.Key), result.Body))
	}
}

func newVerifier(now func() time.Time) *reqsign.Verifier {
	return &reqsign.Verifier{
		Scheme:   reqsign.AccountScheme,
		Verify:   auth.VerifyLinkSignature,
		Now:      now,
		KeyBytes: 32,
	}
}
