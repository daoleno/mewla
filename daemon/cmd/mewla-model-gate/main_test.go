package main

import (
	"strings"
	"testing"
	"time"
)

// A rollout in Codex's shape, invented: a finished turn, a stopped one, and
// one still running, whose turn_context Codex has already written.
const inventedRollout = `{"type":"session_meta","payload":{"cwd":"/work","timestamp":"2000-01-01T00:00:00Z"}}
{"type":"event_msg","payload":{"type":"task_started","turn_id":"a"}}
{"type":"turn_context","payload":{"model":"m-1","effort":"low","turn_id":"a"}}
{"type":"event_msg","payload":{"type":"task_complete","turn_id":"a"}}
{"type":"turn_context","payload":{"model":"m-1","effort":"medium","turn_id":"b"}}
{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"b"}}
{"type":"event_msg","payload":{"type":"thread_settings_applied"}}
{"type":"turn_context","payload":{"model":"m-2","effort":"high","turn_id":"c"}}
`

func TestCodexTurnsCountsATurnAtItsEnd(t *testing.T) {
	created := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	r, mine := codexTurns([]byte(inventedRollout), "/work", created)
	if !mine {
		t.Fatal("the Session's own rollout was not taken as its own")
	}
	if r.turns != 2 || r.last != (ran{"m-1", "medium"}) {
		t.Fatalf("turns %d last %s, want 2 finished turns, the last on m-1/medium", r.turns, r.last)
	}
	finished := inventedRollout + `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"c"}}` + "\n"
	if r, _ := codexTurns([]byte(finished), "/work", created); r.turns != 3 || r.last != (ran{"m-2", "high"}) {
		t.Fatalf("after the running turn ends: turns %d last %s", r.turns, r.last)
	}
	if _, mine := codexTurns([]byte(inventedRollout), "/elsewhere", created); mine {
		t.Fatal("another folder's rollout was taken as the Session's")
	}
	if _, mine := codexTurns([]byte(strings.Replace(inventedRollout, "2000-01-01T00:00:00Z", "1999-12-31T23:00:00Z", 1)), "/work", created); mine {
		t.Fatal("a rollout started long before the Session was taken as its own")
	}
}
