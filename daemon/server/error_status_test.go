package server

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/enrollment"
)

func TestPairErrorStatusUsesSentinels(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{auth.ErrDeviceKeyMismatch, http.StatusConflict},
		{fmt.Errorf("pair: %w", auth.ErrDeviceKeyMismatch), http.StatusConflict},
		{auth.ErrWrongDaemon, http.StatusConflict},
		{auth.ErrInvalidPairingToken, http.StatusUnauthorized},
		{auth.ErrExpiredPairingToken, http.StatusUnauthorized},
		// Text that merely resembles a sentinel is not one.
		{errors.New("device id already paired with a different key"), http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := pairErrorStatus(c.err); got != c.want {
			t.Errorf("pairErrorStatus(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestEnrollmentCreateStatusUsesSentinel(t *testing.T) {
	if got := enrollmentCreateStatus(fmt.Errorf("create: %w", enrollment.ErrTooManyPending)); got != http.StatusTooManyRequests {
		t.Fatalf("limit reached = %d, want 429", got)
	}
	if got := enrollmentCreateStatus(errors.New("too many pending enrollment requests")); got != http.StatusBadRequest {
		t.Fatalf("lookalike text = %d, want 400", got)
	}
	if got := enrollmentCreateStatus(errors.New("invalid enrollment request")); got != http.StatusBadRequest {
		t.Fatalf("other failure = %d, want 400", got)
	}
}
