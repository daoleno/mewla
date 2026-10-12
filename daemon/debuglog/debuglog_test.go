package debuglog

import (
	"bytes"
	"log"
	"testing"
)

func TestParseAcceptsOnlyTrueValues(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "false": false, "junk": false, "1": true, "true": true, " TRUE ": true} {
		if got := parse(value); got != want {
			t.Fatalf("parse(%q) = %t, want %t", value, got, want)
		}
	}
}

func TestPrintfIsSilentUnlessEnabled(t *testing.T) {
	var out bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(previous) })
	wasEnabled := Enabled()
	t.Cleanup(func() { SetEnabled(wasEnabled) })

	SetEnabled(false)
	Printf("routine %d", 1)
	if out.Len() != 0 {
		t.Fatalf("quiet mode printed %q", out.String())
	}
	SetEnabled(true)
	Printf("routine %d", 2)
	if !bytes.Contains(out.Bytes(), []byte("routine 2")) {
		t.Fatalf("verbose mode did not print: %q", out.String())
	}
}
