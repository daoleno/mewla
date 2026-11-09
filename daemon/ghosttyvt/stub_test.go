//go:build !ghosttyvt || !cgo

package ghosttyvt

import (
	"errors"
	"testing"
)

func TestBuildWithoutEngineIsUnavailable(t *testing.T) {
	if Available() || Version() != "" {
		t.Fatalf("Available() = %v, Version() = %q in a build without the engine", Available(), Version())
	}
	if _, err := New(Options{Cols: 80, Rows: 24}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New error = %v, want ErrUnavailable", err)
	}
	if _, err := Restore([]byte("x"), Options{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Restore error = %v, want ErrUnavailable", err)
	}
}
