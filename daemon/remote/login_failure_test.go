package remote

import (
	"errors"
	"net/http"
	"testing"
)

func TestLoginFailureClass(t *testing.T) {
	for err, want := range map[error]string{
		&ServiceError{Status: http.StatusForbidden, Code: "denied"}:     "denied",
		&ServiceError{Status: http.StatusForbidden, Code: "quota"}:      "quota",
		&ServiceError{Status: http.StatusConflict, Code: "route"}:       "conflict",
		&ServiceError{Status: http.StatusGone, Code: "expired"}:         "expired",
		&ServiceError{Status: http.StatusInternalServerError, Code: ""}: "other",
		errors.New("dial tcp: i/o timeout"):                             "network",
	} {
		if got := LoginFailureClass(err); got != want {
			t.Errorf("%v = %s, want %s", err, got, want)
		}
	}
}
