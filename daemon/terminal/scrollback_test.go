package terminal

import (
	"fmt"
	"reflect"
	"testing"
)

func rowsOf(from, to int) []string {
	rows := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		rows = append(rows, fmt.Sprintf("row %d", i))
	}
	return rows
}

func TestOverlapFindsWhatTheCapturesShare(t *testing.T) {
	h := func(rows ...string) []uint64 {
		out := make([]uint64, len(rows))
		for i, r := range rows {
			out[i] = hashRow(r)
		}
		return out
	}
	for _, c := range []struct {
		name        string
		known, next []string
		want        int
	}{
		{"growth", []string{"a", "b", "c"}, []string{"a", "b", "c", "d"}, 3},
		{"scrolled", []string{"a", "b", "c", "d"}, []string{"c", "d", "e", "f"}, 2},
		{"unchanged", []string{"a", "b"}, []string{"a", "b"}, 2},
		{"unrelated", []string{"a", "b"}, []string{"x", "y"}, 0},
		{"first", nil, []string{"x"}, 0},
		{"blank runs", []string{"a", "", ""}, []string{"", "", "", "b"}, 2},
		{"repeating", []string{"x", "y", "x", "y", "x"}, []string{"y", "x", "y", "x", "z"}, 4},
	} {
		if got := overlap(h(c.known...), h(c.next...)); got != c.want {
			t.Errorf("%s: overlap = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestScrollbackTrackerNumbersRows(t *testing.T) {
	var tr scrollbackTracker
	start, reset := tr.advance(80, rowsOf(0, 100))
	if start != 0 || !reset {
		t.Fatalf("first capture: start %d reset %v", start, reset)
	}
	epoch := tr.epoch

	// Twenty rows scrolled off the screen.
	start, reset = tr.advance(80, rowsOf(0, 120))
	if start != 0 || reset || tr.epoch != epoch {
		t.Fatalf("growth: start %d reset %v epoch changed %v", start, reset, tr.epoch != epoch)
	}
	// The oldest rows fell off the end of the buffer: row 50 is now first.
	start, reset = tr.advance(80, rowsOf(50, 220))
	if start != 50 || reset {
		t.Fatalf("eviction: start %d reset %v, want 50 false", start, reset)
	}
	// Nothing new.
	start, reset = tr.advance(80, rowsOf(50, 220))
	if start != 50 || reset {
		t.Fatalf("idle: start %d reset %v", start, reset)
	}
	// Nothing of the old rows left, as after a clear of the scrollback and
	// more output, or a flood bigger than the buffer.
	if _, reset = tr.advance(80, rowsOf(1000, 1100)); !reset || tr.epoch == epoch {
		t.Fatalf("flood did not start a new epoch")
	}
	epoch = tr.epoch
	// A resize reflows every row.
	if _, reset = tr.advance(60, rowsOf(1000, 1100)); !reset || tr.epoch == epoch {
		t.Fatalf("resize did not start a new epoch")
	}
	epoch = tr.epoch
	// The scrollback was erased.
	if _, reset = tr.advance(60, nil); !reset || tr.epoch == epoch {
		t.Fatalf("erase did not start a new epoch")
	}
	// Output after the erase is a fresh start, then grows.
	if start, _ = tr.advance(60, rowsOf(0, 3)); start != 0 {
		t.Fatalf("after erase: start %d", start)
	}
	if start, reset = tr.advance(60, rowsOf(0, 9)); start != 0 || reset {
		t.Fatalf("after erase growth: start %d reset %v", start, reset)
	}
}

func TestNextEpochNeverRepeats(t *testing.T) {
	seen := map[uint64]bool{}
	for i := 0; i < 1000; i++ {
		e := nextEpoch()
		if e == 0 || seen[e] {
			t.Fatalf("epoch %d repeated or zero", e)
		}
		seen[e] = true
	}
	if !reflect.DeepEqual(nonNil(nil), []string{}) {
		t.Fatal("nonNil(nil) is not an empty list")
	}
}
