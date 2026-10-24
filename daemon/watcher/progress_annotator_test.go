package watcher

import (
	"strings"
	"testing"
)

func TestARefusalNoteComesFirstAndKeepsAsMuchOfTheWorkersSummaryAsFits(t *testing.T) {
	note := "⚠ 1 refused action(s) never ran: git push origin main [Data Exfiltration]"
	got := noteThenSummary(note, "Committed locally as 42bd88c; push was refused, remote unchanged.", 160)
	if !strings.HasPrefix(got, note+" | Committed") || len([]rune(got)) > 160 {
		t.Fatalf("got %q (%d runes)", got, len([]rune(got)))
	}
	long := noteThenSummary(note, strings.Repeat("x", 400), 160)
	if !strings.HasPrefix(long, note) || len([]rune(long)) > 160 {
		t.Fatalf("long summary not trimmed: %q (%d runes)", long, len([]rune(long)))
	}
	if got := noteThenSummary(note, "", 160); got != note {
		t.Fatalf("empty summary = %q", got)
	}
	huge := strings.Repeat("n", 300)
	if got := noteThenSummary(huge, "summary", 160); len([]rune(got)) != 160 {
		t.Fatalf("note alone must still fit: %d runes", len([]rune(got)))
	}
}
