package main

import (
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/enrollment"
)

func TestEnrollmentIDForNumberNeedsExactlyOneBrowser(t *testing.T) {
	pending := []enrollment.Request{{ID: "a", Number: "482913"}, {ID: "b", Number: "107755"}}
	if id, err := enrollmentIDForNumber(pending, " 482913 "); err != nil || id != "a" {
		t.Fatalf("one match: %q %v", id, err)
	}
	if _, err := enrollmentIDForNumber(pending, "000000"); err == nil || !strings.Contains(err.Error(), "devices pending") {
		t.Fatalf("no match should say how to look: %v", err)
	}
	clash := append(pending, enrollment.Request{ID: "c", Number: "482913"})
	if _, err := enrollmentIDForNumber(clash, "482913"); err == nil || !strings.Contains(err.Error(), "-id") {
		t.Fatalf("two browsers with one number must not be guessed: %v", err)
	}
}
