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

// refusalCode is the service's own code for a refused refresh, "" when it
// gave none.
func refusalCode(err error) string {
	var refused refreshRefused
	if errors.As(err, &refused) {
		return refused.code
	}
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		return retrieve.ErrorCode
	}
	return ""
}

// clientRefused reports Slack refusing Mewla's client itself, not the
// person's sign-in: signing in again gets a token that stops renewing the same
// way. A standard OAuth invalid_client is not this: for a registered (DCR)
// client it usually means the registration is gone, and signing in again
// registers anew.
func clientRefused(code string) bool {
	return code == "bad_client_secret" || code == "invalid_client_id"
}

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
