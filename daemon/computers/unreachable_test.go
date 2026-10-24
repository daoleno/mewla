package computers

import (
	"errors"
	"fmt"
	"testing"
)

func TestUnreachableErrorIsTypedAndKeepsItsMessage(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	var err error = &UnreachableError{Name: "mac", Cause: cause}
	var typed *UnreachableError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &typed) || !errors.Is(err, cause) {
		t.Fatal("not matchable")
	}
	if got := err.Error(); got != "could not reach mac: dial tcp: refused" {
		t.Fatalf("message changed: %q", got)
	}
}
