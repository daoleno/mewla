package work

import "testing"

// Turn recovery waits on Claude's compact_boundary after sending /compact.
func TestClaudeConversationRecordsWhenItWasCompacted(t *testing.T) {
	b := newClaudeConversationBuilder("s1")
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-18T19:20:43.104Z","sessionId":"s1","message":{"role":"user","content":"Now a long answer"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-18T19:20:43.173Z","sessionId":"s1","isApiErrorMessage":true,"error":"invalid_request","apiErrorStatus":413,"message":{"role":"assistant","content":[{"type":"text","text":"Request too large (max 32MB). Run /compact"}]}}`,
	}
	for i, line := range lines {
		b.consumeLine(i+1, []byte(line))
	}
	if got := b.conversation().CompactedAt; got != "" {
		t.Fatalf("compacted before any compaction: %q", got)
	}
	b.consumeLine(3, []byte(`{"type":"system","subtype":"compact_boundary","uuid":"c1","timestamp":"2026-11-18T19:24:12.367Z","sessionId":"s1","content":"Conversation compacted"}`))
	got := b.conversation().CompactedAt
	if got == "" || got[:19] != "2026-11-18T19:24:12" {
		t.Fatalf("CompactedAt = %q", got)
	}
	// The incremental reader answers with snapshot(), which the turn-recovery
	// supervisor polls; it used to leave the field out.
	if snap := b.snapshot().CompactedAt; snap != got {
		t.Fatalf("snapshot CompactedAt = %q, want %q", snap, got)
	}
}

// A /compact Claude could not carry out is a local_command record with a
// failed outcome; turn recovery reads it instead of waiting out its bound.
func TestClaudeConversationRecordsAFailedCompaction(t *testing.T) {
	b := newClaudeConversationBuilder("s1")
	b.consumeLine(1, []byte(`{"type":"user","uuid":"u1","timestamp":"2026-11-18T19:51:57.734Z","sessionId":"s1","message":{"role":"user","content":"<command-name>/compact</command-name>"}}`))
	b.consumeLine(2, []byte(`{"type":"system","subtype":"local_command","content":"<local-command-stderr>Compaction failed · attached media exceeds size limits</local-command-stderr>","level":"info","timestamp":"2026-11-18T19:51:57.811Z","uuid":"c2","isMeta":false,"commandRun":{"command":"compact","args":""},"commandOutcome":{"kind":"failed"},"sessionId":"s1"}`))
	for name, got := range map[string]CodexConversation{"conversation": b.conversation(), "snapshot": b.snapshot()} {
		if got.CompactedAt != "" || got.CompactFailedAt == "" || got.CompactFailedAt[:19] != "2026-11-18T19:51:57" ||
			got.CompactFailure != "Compaction failed · attached media exceeds size limits" {
			t.Fatalf("%s: compacted %q failed %q %q", name, got.CompactedAt, got.CompactFailedAt, got.CompactFailure)
		}
	}
}
