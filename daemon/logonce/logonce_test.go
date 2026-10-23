package logonce

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
)

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestNoteLogsEachDistinctFailureOncePerScope(t *testing.T) {
	buf := capture(t)
	var l Log
	if !l.Note("calendar save", errors.New("disk full")) {
		t.Fatal("first failure must be logged")
	}
	if l.Note("calendar save", errors.New("disk full")) {
		t.Fatal("repeat failure must not be logged")
	}
	if !l.Note("calendar save", errors.New("read-only")) {
		t.Fatal("a different message must be logged")
	}
	if !l.Note("watch save", errors.New("read-only")) {
		t.Fatal("scopes are independent")
	}
	if got := strings.Count(buf.String(), "WARN"); got != 3 {
		t.Fatalf("logged %d lines, want 3:\n%s", got, buf.String())
	}
}

func TestNoteSuccessRearmsTheScope(t *testing.T) {
	capture(t)
	var l Log
	l.Note("s", errors.New("x"))
	if l.Note("s", nil) {
		t.Fatal("nil error must not log")
	}
	if !l.Note("s", errors.New("x")) {
		t.Fatal("the same failure after a success must be logged again")
	}
}

type pathErr struct{ path string }

func (e *pathErr) Error() string { return "write " + e.path + ": disk full" }
func (e *pathErr) Unwrap() error { return errors.New("disk full") }

func TestNoteComparesRootCauseNotWrappingPaths(t *testing.T) {
	capture(t)
	var l Log
	if !l.Note("save", &pathErr{path: "/tmp/.x.tmp-1"}) {
		t.Fatal("first failure must be logged")
	}
	if l.Note("save", &pathErr{path: "/tmp/.x.tmp-2"}) {
		t.Fatal("the same cause under a different temp name must not be logged again")
	}
}
