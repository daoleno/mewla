package watch

import (
	"testing"
	"time"
)

// A turn's stall evidence and its loss are one incident, so a Worker lost on
// two turns is "lost 2 times" (it read 4), and one lost turn is not a repeat.
func TestWorkerStallsCountsEachTurnOnce(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour)
	signal := func(id, kind, turn string, offset time.Duration) Signal {
		return Signal{ID: id, Source: SourceProcess, Kind: kind, Time: at.Add(offset), Excerpt: kind,
			Refs: map[string]string{"work": "w1", "turn": turn, "event": id}}
	}
	if _, err := store.Append([]Signal{
		signal("s1", "stalled", "turn:aaaaaaaa-0000-0000-0000-000000000001", 0),
		signal("s2", "turn_lost", "turn:aaaaaaaa-0000-0000-0000-000000000001", time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if problems := ruleWorkerStalls(store, time.Now()); len(problems) != 0 {
		t.Fatalf("one lost turn was flagged as repeated: %+v", problems)
	}
	if _, err := store.Append([]Signal{
		signal("s3", "stalled", "turn:aaaaaaaa-0000-0000-0000-000000000002", time.Minute),
		signal("s4", "turn_lost", "turn:aaaaaaaa-0000-0000-0000-000000000002", time.Minute+time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	problems := ruleWorkerStalls(store, time.Now())
	if len(problems) != 1 || problems[0].Count != 2 || len(problems[0].Evidence) != 4 ||
		problems[0].Title != "Worker stalled or lost 2 times: w1" {
		t.Fatalf("problems = %+v", problems)
	}
}
