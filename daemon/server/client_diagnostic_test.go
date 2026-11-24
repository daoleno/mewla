package server

import (
	"strings"
	"testing"
)

func TestClientDiagnosticLineIsOneBoundedLine(t *testing.T) {
	line := clientDiagnosticLine("device-a", clientMessage{
		Name:      "stream gap\nforged line",
		RequestID: "stream-1",
		Body:      "base=7 current=9\n" + strings.Repeat("x", 1000),
	})
	if strings.ContainsAny(line, "\r\n") {
		t.Fatalf("diagnostic must stay one line: %q", line)
	}
	if !strings.HasPrefix(line, "client diagnostic device=device-a event=stream_gap_forged_line stream=stream-1 detail=\"base=7 current=9") {
		t.Fatalf("unexpected line: %q", line)
	}
	if len(line) > 520 {
		t.Fatalf("diagnostic line is not bounded: %d bytes", len(line))
	}
}

// A message that held a connection's read loop is named in its slow line by
// its type alone, never its body.
func TestClientMessageTypeNamesOnlyTheType(t *testing.T) {
	for raw, want := range map[string]string{
		`{"type":"terminal_open","worker_id":"mh-1","cols":82}`: "terminal_open",
		`{"type":"codex conversation\nsubscribe"}`:              "codex_conversation_subscribe",
		`not json`: "unreadable",
	} {
		if got := clientMessageType([]byte(raw)); got != want {
			t.Errorf("type of %q = %q, want %q", raw, got, want)
		}
	}
}
