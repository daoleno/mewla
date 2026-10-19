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
