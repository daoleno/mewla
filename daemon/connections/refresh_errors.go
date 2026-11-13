package connections

import (
	"errors"

	"golang.org/x/oauth2"
)

// refreshRefused: the service answered a refresh and said no (Slack's
// ok:false with its error code).
type refreshRefused struct{ code string }

func (e refreshRefused) Error() string { return "refused: " + e.code }

// refreshUnreachable: a token request that got no answer.
type refreshUnreachable struct{ err error }

func (e refreshUnreachable) Error() string { return "no answer: " + e.err.Error() }
func (e refreshUnreachable) Unwrap() error { return e.err }

// refreshWasRefused reports whether a failed refresh was the service's own
// answer that the sign-in no longer holds. A 5xx answer, a dropped connection
// or a timeout is not: the next use tries again.
func refreshWasRefused(err error) bool {
	var refused refreshRefused
	if errors.As(err, &refused) {
		return true
	}
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		return retrieve.Response == nil || retrieve.Response.StatusCode < 500
	}
	var status StatusError
	if errors.As(err, &status) {
		return status.Status < 500
	}
	return false
}
